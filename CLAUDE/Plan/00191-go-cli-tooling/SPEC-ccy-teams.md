# ccy-teams: functional specification for the Go port

Source of truth: `files/home/.local/bin/ccy-teams.j2` (1938 lines, at commit fd159607). `L<n>` means a
line of that file unless another file is named. Libraries: `files/var/local/claude-yolo/lib/` as
`common-pure.bash` (CP), `session-registry.bash` (SR), `tmux-session.bash` (TS). Helper:
`helpers/agent_bus/declare.py` (DP). Parity oracle: `scripts/test-ccy-teams-command.bash` (TC).
`scripts/test-ccy-teams.bash` tests **ccy's own `--teams` flag** (the launcher and
`lib/agent-bus-seats.bash`), not ccy-teams; it is not an oracle for this port, only for phase 3.

"stderr" is the default for every human-facing message. Only these go to **stdout**: the `--list`
table (L1465-1471), the `--help` text (L114), the declare `CHANGED` line from `create` (L741;
DP L704), the backup listing printed before the no-terminal usage error in `restore` (L1088), and
anything the child commands themselves print to stdout (`agent-bus-install backup-now/restore`,
the plays). Keep it that way (CLAUDE/StderrHygiene.md).

---

## 0. Deployment and the Jinja template

- Deployed by `playbooks/imports/play-claude-yolo.yml` L847-856 (`ansible.builtin.template`,
  dest `/home/{{ user_login }}/.local/bin/ccy-teams`, mode 0755, tags `scripts`, `launchers`).
- **One Jinja variable only**: `{{ root_dir }}` (L96), resolved by the play's `vars:` L10 as
  `{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`, i.e. the directory holding
  `ansible.cfg` = the fedora-desktop checkout the play ran from. TC L82-83 asserts no other Jinja
  token appears anywhere in the template (a bash `${#a[@]}` would open a Jinja comment).
- Dependencies installed by the same play L808-816: `tmux fzf gum wl-clipboard`. `gh` comes from
  `play-git-configure-and-tools.yml` (named at L797). `agent-bus`, `agent-bus-install` and the
  sudoers rule come from `play-agent-bus.yml`.
- **Go port consequence**: ROOT_DIR must be supplied some other way (ldflags `-X` at build time
  by the play, or a sibling config file the play templates, or an env var). TC renders the
  template by text replacement (TC L70-78); for a Go binary TC needs a build step that injects
  `$REPO_ROOT` the same way. Decide this before writing TC changes.

## 1. Commands, flags, usage, exit codes

### 1.1 Usage text (verbatim; `--help` prints it to stdout, exit 0)

The script prints its own header comment lines 2-91 with the leading `# ` stripped (L113-115:
awk from line 2 until the first non-`#` line; a line that is just `#` prints as empty). The
Go binary must embed the same text (TC L500 asserts it contains `ccy-teams -l|--list`; TC L501
asserts it does not contain `set -euo`). The usage block proper (L47-89):

```
Usage:
  ccy-teams                        the picker (needs a terminal)
  ccy-teams -l|--list              print the same table, with a heading, and exit
                                   (colour only on a terminal, and never with NO_COLOR)
  ccy-teams create [--org ORG | --repo OWNER/REPO...] [--team T] [--human NAME]...
                   [--allow-from CIDR]... [--yes]
                                   add a team to agent_bus_teams in the host_vars, with
                                   its Element profile in agent_bus_element_teams, and
                                   install both. On a terminal it asks (with gum) for what
                                   the flags leave out: what the team works on (one
                                   repository, the default in a checkout, which it names;
                                   everything one GitHub owner owns; or several
                                   repositories of any owners, each one you can push to),
                                   all on any branch; the name (the repository's or the
                                   owner's, by default); the people in the team room (you,
                                   by default); other machines' subnets. It shows a
                                   summary and asks once, then writes the entries and runs
                                   play-agent-bus.yml and play-agent-bus-element.yml, and
                                   says it is done only when both are applied. Then it
                                   offers each person's sign-in, opens Element, and, in a
                                   checkout of the team's, starts a ccy session there.
                                   --yes asks and offers nothing: give --org or --repo,
                                   --team and --human. The port is chosen for you
  ccy-teams apply <team> | --all   run play-agent-bus.yml for the team (or every team
                                   out of date or not built), and play-agent-bus-element.yml
                                   for those with an Element profile, then check each now
                                   matches its declaration. --all never takes a team
                                   down: a team declared absent, or built but no longer
                                   declared, needs your decision (ccy-teams remove)
  ccy-teams remove <team>          take a team down, on a terminal only: it says what is
                                   lost, offers a backup first, and keeps the team's data
                                   unless you choose to delete it and type its name
  ccy-teams backup <team>          back the team's homeserver up now
  ccy-teams restore <team> [<n>] [--yes]
                                   restore backup <n> after a yes/no (--yes: none);
                                   without <n>, choose one from the list
  ccy-teams -h|--help              this text
```

The full help additionally contains L2-46 (description, row kinds, keys, sudo) and L85-91
(backups, APPLIED words, `Exit 64 for a usage mistake, 130 when cancelled, 1 for a refusal or a failure.`, `Runs on the HOST, as the desktop user.`). Copy L2-91 verbatim.

### 1.2 Argument dispatch (L122-140, L1203-1224)

| argv[1]                                      | action                                                                      |
| -------------------------------------------- | --------------------------------------------------------------------------- |
| (none)                                       | picker                                                                      |
| `-l` / `--list`                              | list; any further arg -> usage error `'<arg1>' takes no further arguments.` |
| `-h` / `--help`                              | usage to stdout, exit 0 (before the CCY_LIB check)                          |
| `create`/`apply`/`remove`/`backup`/`restore` | subcommand with the remaining args                                          |
| anything else                                | usage error `unknown option '<arg>'.`                                       |

`usage_error <msg>` (L117-120): stderr `ccy-teams: <msg> See ccy-teams --help.`, exit 64.

Then, for every mode except `--help` (L142-149): if `${CCY_LIB}/tmux-session.bash` is missing:

- if `/workspace` is a directory: `ccy-teams runs on the host, not inside a CCY container.`
- else `CCY is not installed here (<CCY_LIB>/tmux-session.bash missing). Run playbooks/imports/play-claude-yolo.yml.`
- exit 1.
  The Go port no longer sources the lib, but TC sets `CCY_LIB`; keep this guard (same file test)
  so the behaviour and messages are unchanged, or replace it deliberately and note it.

`need_checkout` (L158-163), called by create/apply/remove and `collect_teams`: if
`<ROOT_DIR>/helpers/agent_bus/declare.py` is missing -> `ERROR: the fedora-desktop checkout ccy-teams was deployed from (<ROOT_DIR>) has gone; re-run playbooks/imports/play-claude-yolo.yml from where it is now.` exit 1. (`backup` and `restore` do not call it.)

### 1.3 Exit codes (documented: 64 usage, 130 cancelled, 1 refusal/failure; L89)

Actual behaviour, which the port must reproduce (TC asserts the ones marked T):

| code  | when                                                                                                                                                 |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0     | success; Esc/q/Exit in the picker; "no" to offer_create; `restore` picker or confirm declined ("Nothing restored.")                                  |
| 1     | failures and refusals (T many); picker without a terminal (T L618); no at create summary (T L1260); remove wrong name (T L1209)                      |
| 64    | usage errors (T L503, L505, L838, L844, L847, L850, L865, L1189, L1192, L1194, L1196)                                                                |
| 130   | `cancelled` (Esc/Ctrl-C in a gum question, T L1250, L1255); Ctrl-C (SIGINT trap) in the picker; a 130 from in_picker                                 |
| 78    | **leaks from declare.py** under `set -e`: e.g. `create --yes` with no bus address (T L1029 asserts 78)                                               |
| other | any non-zero from a child run unguarded under `set -e` propagates: declare.py (64/78), `agent-bus-install backup-now`/`restore` (1/64/77/78), `sudo` |

Unguarded call sites whose status is the process exit status: L629 (`address`, `|| exit`), L722
(`create --dry-run`, `|| exit`), L741 (`create`), L744 (`element`), L1017 (`remove`), L1040
(`backup-now`), L1082 (`backups`), L1107 (`restore`), L1571 (`drop-limit`), L1562 (`limited`,
`|| exit 1`), and open_element's `launch_element` (L1800; a failed launch exits 1, T L772).

### 1.4 Constants

- `MAX_TRIES=3` (L111): ask_valid retries, and the picker's action counter.
- `REPOS_MAX=32` (L587; = `protocol.REPOS_MAX`).
- `SUDO_AGENT_BUS=/usr/local/bin/agent-bus`, `SUDO_INSTALLER=/usr/local/sbin/agent-bus-install`
  (L109-110): always invoked by absolute path through sudo (sudo's secure_path may lack
  /usr/local). TC asserts the absolute paths in the sudo log.
- `ACCENT=3` (L189): terminal colour index used for gum and the step headings.
- Colours (L182-190): reset `\e[0m`, bold `\e[1m`, green `\e[32m`, yellow `\e[33m`, red `\e[31m`,
  accent `\e[33m`. `print_error` (CP L25-27) is `echo -e "\033[0;31mERROR:\033[0m <msg>" >&2`.

## 2. Flows

### 2.1 Colour and the small output helpers

- `colour_on <fd>` (L179-181): fd is a TTY and `NO_COLOR` is empty/unset.
- `say <text>` (L205-211): stderr; bold when `colour_on 2`.
- `word_colour <word>` (L194-202), first match wins (bash `case` order):
  - yellow: `free`, `invited`, `no`, `parked`, `never signed in`, `*(stopped)`
  - green: `up`, `active`, `held`, `in room`, `yes`, `* signed in`
  - yellow: `out of date*`, `not built`
  - green: `up to date`
  - red: `down*`, `not installed`, `not in room`, `unknown`, `need sudo`, `decide:*`, `unreadable`
  - anything else: no colour (e.g. `removed`, session names, `-`).
- `step <title> <hint>` (L405-415): increments STEP; prints blank line then
  `"<STEP>/<STEPS>  <title>"` (bold+accent on a TTY: `\e[1m\e[33m<n>/<m>  <title>\e[0m`), then
  `explain <hint> 5`.
- `explain <text> [indent]` (L395-401): width = `tput cols` (if not a number or < 44, use 80);
  runs `gum style --width <cols-4> --padding "0 0 0 <indent|0>" <text>` to stderr.
- `answered <value>` (L418-424): `     ✓ <value>` (5 spaces; ✓ green on a TTY).

### 2.2 sudo

`sudo_ready <why>` (L218-234):

1. `sudo -n -v` (stderr+stdout captured as `probe`). Success -> return 0, nothing said.
2. If stdin or stderr is not a TTY: `ERROR: <why> That needs sudo, and with no terminal there is nowhere to ask for your password (sudo said: <probe>). Run sudo -v first.` return 1.
3. Else: blank line, `say <why>`, `sudo asks for your login password.`, run `sudo -v` (interactive).
   Failure: `ERROR: sudo was not given your password (three wrong tries, or cancelled).` return 1.

`as_root <cmd...>` (L1298-1311): runs `sudo -n <cmd...>`, stdout captured into ROOT_OUT, stderr to a
temp file. Result: 0 ran; **2** when the exit is 1 *and* the whole stderr starts with `sudo: `
(sudo wanted a password; nothing printed); otherwise 1 with the stderr copied to our stderr.
Used only for the two read-only commands covered by the sudoers rule.

The sudoers rule (`files/etc/sudoers.d/agent-bus-read-only.j2`, installed by play-agent-bus.yml):

```
{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/agent-bus ^list [a-z][a-z0-9-]*$, \
    /usr/local/bin/agent-bus ^human devices [a-z][a-z0-9-]* [a-z][a-z0-9_-]*$
```

TC L379-395 parses this file and its fake sudo grants exactly these regexes: the Go port must
call exactly `sudo -n /usr/local/bin/agent-bus list <team>` and
`sudo -n /usr/local/bin/agent-bus human devices <team> <name>` (no extra flags, no `--`).

### 2.3 Collecting the teams (`collect_teams`, L1161-1200)

Globals reset: TEAMS (ordered list), PORT_OF, URL_OF, SERVER_OF, LOADED.

1. `need_checkout`.
2. `declare_helper declared --file <HOST_VARS>`; failure -> `ERROR: the teams declared in <HOST_VARS> could not be read (the reason is above).` return 1 (declare printed its own
   `declare: ...` line). Each line `TEAM\t<team>\t<port|->\t<present|absent>\t<server_name>\t<url|->`:
   only `present` teams are added, in file order; PORT_OF=port (may be `-`... see note), SERVER_OF,
   URL_OF (unless `-`).
   Note: a present team with no port yields PORT_OF `-`; the WHERE column then says `port -`.
3. `systemctl list-units --all --plain --no-legend --type=service 'agent-bus-hs@*'`; failure ->
   `ERROR: systemctl could not list the agent-bus homeserver units.` return 1. For each line,
   first field matching `agent-bus-hs@*.service`: team = the middle part; LOADED[team]=1; if not
   yet seen, appended to TEAMS.
4. Every `<APPLIED_DIR>/*.json` (glob order = sorted): team = basename without `.json`; appended if
   not seen. (Only the name is used here; the content is validated by `declare status`.)

### 2.4 Applied state (`read_status`, L831-867)

`declare_helper status --file <HOST_VARS> --source <ROOT_DIR> --applied <APPLIED_DIR> [--installed <team>]...` (one `--installed` per LOADED team; hash order). Failure ->
`ERROR: whether each team is applied as declared could not be read (the reason is above).`
return 1. Lines `STATUS\t<team>\t<state>\t<detail>` fill STATUS_OF / DETAIL_OF.

`status_word <team>` (L850-867):

| state      | detail                  | word                    |
| ---------- | ----------------------- | ----------------------- |
| current    |                         | `up to date`            |
| outdated   | unstamped               | `out of date`           |
| outdated   | config,software         | `out of date: both`     |
| outdated   | other (config/software) | `out of date: <detail>` |
| unbuilt    |                         | `not built`             |
| removed    |                         | `removed`               |
| decide     | absent                  | `decide: absent`        |
| decide     | anything else           | `decide: undeclared`    |
| unreadable |                         | `unreadable`            |
| (none)     |                         | `unknown`               |

`refresh` (L870-873) = reset team globals, `collect_teams && read_status`.

### 2.5 The table (`collect`, L1365-1451)

Resets every R\_\* and collection global, INCOMPLETE, NEED_SUDO. Then:

1. `collect_teams || return 1`; if TEAMS empty return 0 (no rows).
2. `read_status || return 1`.
3. `collect_sessions || return 1` (2.6).
4. For each checkout in CHECKOUTS (2.6 order): `seat_lines <dir>` = if
   `<dir>/.claude/ccy/pingbus/seats` is a directory, run `agent-bus seat list` \*\*as the user, cwd
   <dir>** (no sudo). Non-zero -> `ERROR: agent-bus seat list could not read every seat in <dir>
   (above); those are left out.` and INCOMPLETE=`a seat is unreadable`; the lines it did print are
   still used. Each non-empty line is kept with its dir.
5. For each team in TEAMS order:
   - `where = "port <PORT_OF|unknown (not declared)>"`.
   - Not LOADED: state `not installed`, MEMBERS_UNKNOWN=`not installed`; where +=
     `, not declared present: ccy-teams remove <team> takes it down` when STATUS_OF is `decide`,
     else `, install it: ccy-teams apply <team>`.
   - LOADED: state = `homeserver_state` (2.7). `up` -> `collect_members` (2.8); else
     MEMBERS_UNKNOWN=`homeserver down`. If MEMBERS_UNKNOWN set: where += `, members: <reason>`.
   - INCOMPLETE set: where += `, <INCOMPLETE> (above)`.
   - STATUS_OF unreadable: where += `, entry unreadable: <DETAIL_OF>`.
   - **Team row**: `add_row team - team <state> - - <where> - - <status_word>`.
   - activity_of[handle] = 5th field of each `MEMBER` line (`active`/`parked`). Default seat
     activity: `unknown`, or `need sudo` when MEMBERS_UNKNOWN is `need sudo`.
   - **Seat rows**, in seat-line order: each `SEAT\t<team>\t<seat>\t<held|free>\t<self|->\t<handle>`
     whose team matches: mark handle shown; session = SEAT_SESSION\["<dir>|<seat>@<team>"\],
     restore = SEAT_RESTORE[same] or `-`. No session -> session = seat state (`held`/`free`),
     restore `-`. Session not live (not in LIVE) -> session += `(stopped)` (no space).
     \`add_row team <seat> ccy \<activity_of[handle] or default> <session> <restore>
     <dir with $HOME->~> <handle> <dir>`.
   - **Other member rows**: each `MEMBER\t<handle>\t<role>\t<membership>\t<active|parked>` not
     shown as a seat: kind = text after the last `.` of the handle, `podman` -> `ccy`;
     `add_row team <handle> <kind> <active|parked> - - <role> <handle> -`.
   - **Human rows**: each `HUMAN\t<name>\t<membership>`: `add_row team <name> human <room_word> <SIGNED_IN[team|name] or unknown> - "Element; Enter: new password" - -`.
     room_word (L1357-1363): `join`->`in room`, `invite`->`invited`, else `not in room`.

Row format (L1118-1123): `printf '%-24s  %-36s  %-6s  %-13s  %-18s  %-26s  %-7s  %s'` over
TEAM MEMBER KIND STATE APPLIED SESSION RESTORE WHERE. `add_row` (L1146-1156) arguments are
`<team> <member> <kind> <state> <session> <restore> <where> <handle> <dir> [applied]`, applied
defaulting to `-`. Painted row (L1137-1143): the team cell `%-24s`, bold when kind is `team`;
STATE/APPLIED/SESSION/RESTORE cells padded to 13/18/26/7 *inside* their colour codes. Plain text
of a painted row equals R_TEXT (the picker relies on this). Parallel arrays per row: R_TEXT,
R_SHOWN, R_TEAM, R_NAME, R_KIND, R_SESSION, R_HANDLE, R_DIR.

### 2.6 Sessions (`collect_sessions`, L1239-1282)

1. `ccy_tmux_list` (TS L46-57): `tmux -L ccy list-sessions -F '#{session_name} #{session_attached} #{session_path}'`. Output containing `no server running` or `No such file or directory` on
   failure = no sessions; other failure -> `ERROR: tmux list-sessions failed: <out>`, return 1.
   Each line: LIVE[name]=attached count; names starting `ccy-` add their path to CHECKOUTS.
2. `ccy_registry_dir` (SR L54-71): `$CCY_STATE_DIR/sessions` if set, else
   `${XDG_STATE_HOME:-$HOME/.local/state}/ccy/sessions`; a relative XDG_STATE_HOME or base is an
   error (`ERROR: XDG_STATE_HOME='<v>' is not absolute, so the session registry's location would depend on the current directory.`), return 1.
3. Every file there (glob order) not matching `*.tmp.*`: `ccy_registry_read` (format in 3.4). Read
   failure -> `ERROR: the session record <file> could not be read; its seats show no session.`
   INCOMPLETE=`a session record is unreadable`, continue. Records with prefix other than `ccy`
   are skipped. REC_DIR added to CHECKOUTS. The launcher args are scanned up to `--` for
   `--teams <list>` or `--teams=<list>` (last wins); each comma-separated ref sets
   SEAT_SESSION\["\<REC_DIR>|<ref>"\]=REC_NAME and SEAT_RESTORE[same]=REC_RESTORE.
4. If cwd is in a git work tree: `git rev-parse --show-toplevel` added to CHECKOUTS.
   CHECKOUTS is de-duplicated, in first-seen order.

### 2.7 `homeserver_state <team>` (L1227-1235)

`systemctl is-active agent-bus-hs@<team>.service`: exit 0 and output `active` -> `up`; otherwise
`down (<output or unknown>)` (e.g. `down (inactive)`; TC L535).

### 2.8 `collect_members <team>` (L1316-1354)

`as_root /usr/local/bin/agent-bus list <team>`: 0 -> MEMBERS_OF[team]=stdout; 2 ->
MEMBERS_UNKNOWN `need sudo`, NEED_SUDO += ` <team>`; 1 -> MEMBERS_UNKNOWN `read failed (above)`.
On 0, for each `HUMAN\t<name>\t...` line: `as_root /usr/local/bin/agent-bus human devices <team> <name>`: 0 -> count lines starting `DEVICE`: 0 -> `never signed in`, n -> `<n> signed in`;
2 -> `need sudo` (and NEED_SUDO += team); 1 -> `unknown`.

### 2.9 `--list` (L1458-1482)

1. `collect || exit 1`.
2. No rows: stderr `No agent teams on this machine yet. ccy-teams create starts one.` exit 0.
3. NEED_SUDO set: stderr `The members of<NEED_SUDO> are not shown: reading them needs root. Run sudo -v first, or run play-agent-bus.yml once, which installs a rule that lets ccy-teams read them without a password.` (NEED_SUDO starts with a space: `members of alpha`; a team can appear twice).
4. stdout TTY and colour on: bold heading line, then R_SHOWN rows. Otherwise: heading, **a blank
   line**, R_TEXT rows (oddity 8.10).
5. Exit 1 if any MEMBERS_UNKNOWN is `need sudo` or starts `read failed`, or INCOMPLETE or NEED_SUDO
   is set; else 0. `--list` never runs `sudo -v` (TC L541).

### 2.10 The picker: start-up (L1485-1630, L1850-1889)

1. stdin and stdout must be TTYs, else stderr `ccy-teams needs a terminal: it attaches sessions to the one you are in. ccy-teams --list prints the table instead.` exit 1.
2. SIGINT -> `Cancelled; nothing was changed.` exit 130 (L1489).
3. `collect_teams || exit 1` (no sudo needed). No teams -> **offer_create** (L1492-1508):
   `need_gum`; say `No agent teams on this machine yet.`; `gum confirm --default=false "Create one now?"`: 0 -> `cmd_create` (no args) and exit with its status; 1 -> `Nothing created. ccy-teams create starts one whenever you like.` exit 0; other -> cancelled (130).
4. **offer_path_limits** (L1559-1579): `declare_helper limited --file <HOST_VARS>` (failure: exit 1).
   For each `LIMITED\t<team>\t<prefixes>\t<trusts>` line with no marker
   `<DECLINED_DIR>/path-limit-<team>`: need_gum; say `One question about a team already on this machine, asked once, whichever checkout you are in.`; say `Team <team> (for <trusts>) was made when teams were limited to some folders: its agents may only point each other at files under <prefixes>.`; `ask_yes "Remove the limit, so any path in its repositories can be named?" true`:
   yes -> `declare_helper drop-limit --file <HOST_VARS> --team <team>` (stdout discarded; failure
   exits) then `in_picker apply_teams <team>`; no -> mkdir DECLINED_DIR, create the empty marker,
   `Kept. ccy-teams will not ask about <team>'s limit again.`
   DECLINED_DIR = `${XDG_STATE_HOME:-$HOME/.local/state}/ccy-teams/declined` (L107).
5. **offer_update** (L1538-1555): `read_status || exit 1`; teams with STATUS_OF outdated/unbuilt,
   as `<team> (<status_word>)`, hash order. None -> return. need_gum; say `Not as declared: <list space-joined>.`; `ask_yes "Update them now (play-agent-bus.yml)?" true`: yes -> `in_picker cmd_apply --all` then `back_to_list`.
6. Header (built once, L1850-1865):
   - restore_word `on` if `$HOME/.config/systemd/user/default.target.wants/ccy-sessions-restore.service`
     exists, else `off`.
   - new_line: in a git work tree `Ctrl-N starts a new ccy session in the row's team, in <here>`,
     else `Ctrl-N unavailable: <here> is not a project folder (no git repository)`; here = $PWD with
     $HOME -> `~` (L1630).
   - `ccy_tmux_header` of: keys line `Enter attach a seat / password for a person   Ctrl-P password Ctrl-E Element   ? help   Esc or q quit` (three spaces between groups, as in L1861), then
     `Ctrl-N new session   Ctrl-U update out-of-date teams   Ctrl-R restore on/off   Ctrl-X remove/park/take down`, new_line, `RESTORE: whether the seat's session is started again after a reboot; session restore on this machine is <on|off> (ccy_restore_sessions)`, and the column
     heading (bold when stdout colour is on). The keys line is prefixed `↑↓ choose   ` (TS L797).
7. Loop (L1869-1938):
   1. `collect || exit 1`.
   2. NEED_SUDO: `sudo_ready "<READ_WHY>"` (READ_WHY L1867: `Reading who is in each team, and whether its people are signed in, is done as root: the team's admin token is root's. Once play-agent-bus.yml has run, a sudoers rule lets ccy-teams read this without a password; until then, sudo asks.`). Refused -> `ERROR: so the teams' members cannot be shown, and nothing was changed. ccy-teams --list shows the teams without them.` exit 1. Then `collect || exit 1`;
      still NEED_SUDO -> `ERROR: sudo took your password but still refuses agent-bus list, so the members cannot be shown.` exit 1.
   3. No rows -> offer_create.
   4. First pass only: **offer_join_here** (L1585-1608): not in a work tree -> skip.
      `join_candidates` (L771-777: `the_remote_repo`, then `declare_helper teams-for --file <HOST_VARS> --repo <owner/repo>`, the `TEAM\t<name>` lines). Failure -> skip. Empty ->
      **offer_create_here** (L1613-1628): repo empty -> skip; need_gum; say `No team on this machine works on <repo> (this checkout). The teams listed next are for other repositories.`;
      `gum confirm --default=true "Create a team for <repo> now?"`: 0 -> cmd_create, exit with its
      status; 1 -> `Nothing created. ccy-teams create starts one whenever you like.` (continue);
      else cancelled. Non-empty: for each candidate team that is LOADED and `up`, with no key
      `<toplevel>|*@<team>` in SEAT_SESSION whose session is live: need_gum; `offer_join <team>`
      (2.12.9).
   5. Rows = R_SHOWN (stdout colour on) or R_TEXT; `ccy_tmux_pick "Agent teams" <HEADER> "ctrl-n,ctrl-r,ctrl-x,ctrl-e,ctrl-p,ctrl-u,?"`. Non-zero (Esc, q, Exit row) -> exit 0.
   6. key = first line, row = second; `row_index` = first index whose R_TEXT equals the row; none
      -> `That row has changed; the list has been refreshed.` continue.
   7. Dispatch on `<key>:<kind>` (L1904-1932), first match:
      | match                | action                                                                      | then              |
      | -------------------- | --------------------------------------------------------------------------- | ----------------- |
      | `ctrl-n:*`           | new_session R_TEAM (2.11.2)                                                 | counts an attempt |
      | `ctrl-r:*`           | toggle_restore (2.11.4)                                                     | continue          |
      | `ctrl-x:*`           | remove_or_park (2.11.5)                                                     | continue          |
      | `ctrl-e:*`           | open_element R_TEAM (2.11.6)                                                | counts an attempt |
      | `ctrl-p:*`, `:human` | password_row (2.11.3)                                                       | continue          |
      | `ctrl-u:*`           | update_all: `in_picker cmd_apply --all`, back_to_list                       | continue          |
      | `?:*`, `:team`       | show_help (2.11.7)                                                          | continue          |
      | `:ccy`               | attach_seat (2.11.1)                                                        | counts an attempt |
      | anything else        | `Enter on <R_NAME> does nothing: Ctrl-X parks a member. ? shows every key.` | continue          |
   8. Counted: attempt++ (starts at 1); > MAX_TRIES -> `Nothing attached or started after 3 tries.` exit 1 (oddity 8.4).

`in_picker <cmd...>` (L1519-1527): run in a subshell (its exit ends only it). 0 ok; 130 -> exit 130;
other -> `(<cmd> failed, exit <rc>: see above.)` and continue (oddity 8.12: internal function
name shown).

`back_to_list` (L1531-1536): `Press Enter to go back to the list.` then read a line from
`/dev/tty`; on read failure print a newline. (TC drives /dev/tty with EOF.)

### 2.11 Picker keys

#### 2.11.1 Enter on a ccy row: `attach_seat` (L1645-1661)

- session = R_SESSION (may be `held`, `free`, `-`, `name(stopped)`). Not in LIVE -> `Seat <R_NAME>@<R_TEAM> has no running session to attach. Ctrl-N starts one.` return.
- LIVE count != 0 -> `'<session>' is open in another terminal. ccy-sessions takes it over (Ctrl-T).`
- `ccy_tmux_attach <session>` (TS L115-140): `_ccy_tmux_clients` = `tmux -L ccy list-clients -t =<name> -F '#{client_tty} #{client_activity}'` (failure mentioning `can't find session`, `no server running` or `No such file or directory` = gone (2); other failure error, 1). Not detached
  -> `ERROR: '<name>' is open in another terminal, or gone. It can be attached from one terminal only.` return 2. Then `tmux -L ccy set-hook -g client-attached "<hook>"` where hook =
  `if-shell 'test $(tmux -L ccy list-clients -t "#{session_name}" | wc -l) -gt 1' 'detach-client'`;
  `Attaching to '<name>'.`; `tmux -L ccy attach-session -t =<name>` (failure: `ERROR: could not attach to '<name>'`, 1). After detach: clients gone (2) -> `'<name>' has ended.` 0; error -> 1;
  clients present -> `ERROR: '<name>' is open in another terminal, so this terminal was not attached to it.` 3; else `Detached. '<name>' keeps running; run ccy in its project directory to return to it.` 0.
- rc 0 -> exit 0; 3 -> nothing more; other -> `'<session>' was taken or ended just now — the list has been refreshed.`

#### 2.11.2 Ctrl-N: `new_session <team>` (L1665-1701)

1. Not in a work tree: `<here> is not a project folder (no git repository), so ccy cannot be started here.` return.
2. `homeserver_state` not `up`: `<team>'s homeserver is down, so no session can join it.` return.
3. top = `git rev-parse --show-toplevel`. `seat_lines <top>` failure: `Not every seat of this checkout could be read (above), so no seat is offered: one of them may be the seat a new session would take.` return.
4. next = 1 + the largest numeric seat among this checkout's seats of the team, and among
   MEMBERS_OF[team] `MEMBER` handles matching `\.([0-9]+)\+[^+]*\.podman$` (any repo; oddity 8.17).
   Rows: `Seat <seat>@<team>   free` per free seat of the team (three spaces), then
   `Seat <next>@<team>   new`.
5. `ccy_tmux_pick "New ccy session in <team>" "$(ccy_tmux_header "Enter start   Esc or q back" "Runs ccy --teams <seat>@<team> in <top ~>; a new seat is created by that launch (sudo).")" ""`.
   Cancelled -> return.
6. Picked `Seat X@team ...` -> ref `X@team`; `Starting ccy --teams <ref> in <top ~>.`; chdir top;
   **exec** `<CCY_LAUNCHER> --teams <ref>`.

#### 2.11.3 Enter on a human / Ctrl-P: `password_row` (L1760-1767)

- Not a human row: `Ctrl-P sets a person's Element password: choose one of the team's people (KIND human).` return (no back_to_list).
- `set_password team name` (L287-295): `ccy_tmux_confirm "New Element password for <name>?" "Sets a new password for <name> on <team>'s homeserver and shows it here, once: it is stored nowhere. <name>'s other Element sessions are signed out." "set a new password for <name>"` (cursor on Exit).
  No -> `No password was set for <name>.` Yes -> `new_password` (L272-283): `sudo_ready "A password is set as root, by agent-bus (the team's admin token is root's)."` failure -> `No password was set for <name>.`; `sudo -n /usr/local/bin/agent-bus human password <team> <name>` (stdout = the
  password) failure -> `No password was set for <name>, for the reason above.`; then `show_login`.
- Then back_to_list.

`show_login <team> <name> <password>` (L242-268), all stderr:

```

<say>Element sign-in for <name>, team <team>
  Homeserver  <URL_OF or "unknown (not declared)">
  Username    <name>
  Matrix ID   @<name>:<SERVER_OF or <team>.agent-bus.internal>
  Password    <password, bold on a TTY>
Shown this once and stored nowhere. The team's Element profile has the homeserver set already.
```

(`printf '  %-11s %s\n'`.) Then only if WAYLAND_DISPLAY is set: wl-copy missing -> `ERROR: wl-copy is not installed, so the password cannot be put on the clipboard; copy it from above. playbooks/imports/play-claude-yolo.yml installs wl-clipboard.`; else `ccy_tmux_confirm "Copy the password?" "Put <name>'s new password on the clipboard, to paste into Element. A clipboard manager may keep a copy." "copy the password" yes` (cursor on Yes); yes -> password on **stdin** of `wl-copy`
(never argv; TC L803): ok `The password is on the clipboard.`, fail `ERROR: wl-copy failed (above), so the password is not on the clipboard; copy it from above.`

#### 2.11.4 Ctrl-R: `toggle_restore <i>` (L1704-1714)

key `<R_DIR>|<R_NAME>@<R_TEAM>`; not kind ccy or no SEAT_RESTORE -> `Ctrl-R changes whether a seat's ccy session comes back after a reboot, and this row has no session.` Else want = `no` if current is
`yes`, else `yes`; `ccy_registry_set_restore <R_SESSION minus "(stopped)"> <want>` (SR L486-517: re-
reads and rewrites the record with only `restore=` changed, keeping compose and args; says `Session <name> will be started again after a reboot.` / `Session <name> will not be started again after a reboot.`; errors: no record `ERROR: session <name> has no record in <dir>: it has ended, or ccy did not start it under tmux, so there is no restore to change.`, unreadable, ended mid-change).

#### 2.11.5 Ctrl-X: `remove_or_park <i>` (L1718-1756)

- kind `team`: `in_picker cmd_remove <team>` then back_to_list.
- kind `human`: `A team's people are its agent_bus_teams entry's humans: change them there, then ccy-teams apply <team>.`
- kind `ccy` with a dir: `ccy_tmux_confirm "Remove seat <seat>@<team>?" "<handle> is parked (its token revoked; its history kept) and the seat's directory in <dir ~> deleted. Launching into <seat>@<team> again brings it back." "remove <seat>@<team>"`; yes -> in `<dir>`: `agent-bus seat remove <seat>@<team>` (as the user); failure -> `The seat was not removed, for the reason above.`
- kind `ccy` without a dir (`-`) and INCOMPLETE set: `<handle> may be one of the seats that could not be read (above), so it is not parked here: remove it with agent-bus seat remove in its checkout.` Otherwise falls through to park:
- any other kind: `ccy_tmux_confirm "Park <handle>?" "Its token is revoked; its account, role and room are kept. add-member --seat returns it, and an agent_bus_lxc_members entry is returned by the next play run unless set absent." "park <handle>"`; yes -> `sudo_ready "A member is parked as root, by agent-bus."` (fail: `<handle> was not parked.`) then `sudo -n /usr/local/bin/agent-bus park-member <team> <handle>` (fail: `<handle> was not parked, for the reason above.`).

#### 2.11.6 Ctrl-E: `open_element <team>` (L1772-1802)

1. launcher = `$HOME/.local/share/applications/agent-bus-<team>-element.desktop`. Missing: need_gum;
   `<team> has no Element profile on this desktop yet.`; `ask_yes "Set one up now? It installs Element Desktop if needed (sudo asks for your password)." true`; no -> `Nothing set up. Ctrl-E offers it again.` return. Yes -> `set_up_element` (L351-354: `declare_helper element --file <HOST_VARS> --team <team>` then `run_element_play <team>`); failure -> `<team> was not set up in Element, for the reason above.`, back_to_list, return.
2. For each human row of the team whose SESSION column is `never signed in`: `<name> has never signed in to <team>.`; `set_password`; on success `Copy it now. Press Enter to open Element.` and read a
   line from /dev/tty (failure prints a newline).
3. `Opening <team> in Element.`; `launch_element <team>`; exit 0 (launch failure exits 1, via set -e).

`run_element_play <team>...` (L324-337): say `Running play-agent-bus-element.yml: it installs Element Desktop (sudo) and writes the Element profile for <teams>.`; run `<ELEMENT_PLAY>` with **no
arguments**; failure -> `ERROR: play-agent-bus-element.yml failed (above), so <teams> has no Element profile yet.` 1; then each team's launcher must exist, else `ERROR: play-agent-bus-element.yml finished, but <team>'s Element launcher is not there.` 1.

`launch_element <team>` (L308-319): log = `${XDG_STATE_HOME:-$HOME/.local/state}/ccy-teams/element- <team>.log` (mkdir -p its dir); run `systemd-run --user --scope --quiet --collect --unit ccy-teams- element-<team>-<pid> --description "Element for agent team <team>" -- setsid -w gio launch <launcher>`
with stdin /dev/null and stdout+stderr **appended** to the log. Failure -> `ERROR: Element did not start; its messages are in <log ~>:` then the log's last 5 lines to stderr, return 1. Success ->
`Element is starting; its own messages go to <log ~>.` (The .desktop's `Exec=flatpak run im.riot.Riot --profile <team>` is written by `helpers/agent_bus/element.py` L190 via the element
play; ccy-teams itself never calls flatpak.)

#### 2.11.7 `?` / Enter on a team: `show_help` (L1811-1848)

Blank line; say `ccy-teams: the agent teams on this machine`; then this text verbatim (stderr),
then back_to_list:

```
Each team is a bold row: its homeserver (up, down, or not installed yet), its port, and
APPLIED, whether it runs what the host_vars declare:
  up to date          as declared
  out of date: ...    the declaration (config), the bus software, or both changed since;
                      Ctrl-U (or ccy-teams apply) brings it up to date
  not built           declared, never installed: Ctrl-U builds it
  decide: absent      declared absent but still running; decide: undeclared, running but
                      no longer declared. Never updated in bulk: Ctrl-X on the team takes
                      it down (its data kept unless you delete it)
Under it, its members and people:
  ccy      a seat of a ccy checkout: the session in it, and RESTORE, whether that
           session is started again after a reboot (yes or no)
  lxc ...  another member: active, or parked (its token revoked)
  human    a person in the team room: in room, invited or not in room, and how
           many Element sessions they are signed in with

Keys:
  Enter    a seat: attach its session; a person: a new Element password; a team: this
  Ctrl-N   start a ccy session in a free or new seat of the team, in this folder
  Ctrl-P   a new Element password for the person on the row (shown once; their other
           Element sessions are signed out)
  Ctrl-E   open the team in Element
  Ctrl-U   update every team out of date or not built (never takes one down)
  Ctrl-R   restore after a reboot, on or off, for the seat's session
  Ctrl-X   remove a seat, park a member, or take a team down (after a yes/no; a
           backup first, and the data kept unless you type the team's name)
  ?        this help          Esc or q   leave

Colours: green is working, yellow wants a look, red is not working.
"need sudo": reading the members needs root; play-agent-bus.yml installs a rule that
lets ccy-teams read them without a password.
```

### 2.12 create (`cmd_create` = need_checkout + `create_wizard`, L593-752)

#### 2.12.1 Flags (L596-613)

`--yes`; `--team V`, `--human V` (repeatable), `--org V` (lower-cased), `--repo V` (repeatable,
lower-cased), `--allow-from V` (repeatable). Missing value -> usage `create's <flag> needs a value.`;
anything else -> usage `unknown create option '<arg>'.`

#### 2.12.2 Preparation

- `here` = `the_remote_repo` (L539-544): in a work tree, `git remote get-url origin` (failure ->
  empty) -> `declare_helper github-remote --url <url>` (lower-case `owner/repo` or nothing).
- Not `--yes`: stdin and stderr must be TTYs, else usage `'create' asks its questions on a terminal; without one give every value as a flag, and --yes.` need_gum; need_gh unless --org/--repo given;
  `gum style --bold --border rounded --padding "0 1" "New agent team"`; explain `Agents and people working together in one private room on this machine, which you follow in Element. Ctrl-C leaves at any point; nothing changes until you say yes at the end.`
- STEPS = +2 if no org/repos, +1 no team, +1 no humans, +1 no allow-from, then known_address =
  `declare_helper address --file <HOST_VARS>` (status propagates), +1 if empty.

`need_gum` (L378-391): `gum` missing -> `ERROR: gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.` exit 1. Exports
(TC L983-988 asserts): `GUM_INPUT_HEADER_FOREGROUND="" GUM_INPUT_PROMPT_FOREGROUND=3 GUM_INPUT_PLACEHOLDER_FOREGROUND="" GUM_INPUT_CURSOR_FOREGROUND=3 GUM_CONFIRM_PROMPT_FOREGROUND="" GUM_CONFIRM_SELECTED_FOREGROUND=0 GUM_CONFIRM_SELECTED_BACKGROUND=3 GUM_CONFIRM_UNSELECTED_FOREGROUND="" GUM_CONFIRM_UNSELECTED_BACKGROUND="" GUM_INPUT_SHOW_HELP=false GUM_CONFIRM_SHOW_HELP=false GUM_CHOOSE_SHOW_HELP=false GUM_SPIN_TITLE_FOREGROUND="" GUM_SPIN_SPINNER_FOREGROUND=3 GUM_CHOOSE_HEADER_FOREGROUND="" GUM_CHOOSE_ITEM_FOREGROUND="" GUM_CHOOSE_CURSOR_FOREGROUND=3 GUM_CHOOSE_SELECTED_FOREGROUND=3` (empty values must be *set*, not
unset; no value of two or more digits).

`need_gh` (L795-802): `gh` missing -> need_tool message naming `playbooks/imports/play-git- configure-and-tools.yml`; `gh auth status` failing -> `ERROR: gh is not logged in to GitHub, and the org is looked up there (gh said: <out>). Run gh auth login, then try again.` exit 1.

#### 2.12.3 Questions (only for what flags leave out, only without --yes)

Each `gum input` call: `gum input --header <H> --placeholder <P> --value <V> --width 0`. Gum exit 1
(Esc) or 130 (Ctrl-C) = cancel. `ask_valid <checker> <H> <P> <V>` (L436-451): up to 3 tries; the
answer is trimmed of leading/trailing whitespace; the checker writes its problem to stderr; a refused
answer becomes the next `--value`; after 3: `Giving up after 3 tries; nothing was changed.` -> exit 1.
Cancel -> `Cancelled; nothing was changed.` exit 130. `say_problem <msg>` prints
`  <msg minus a leading "declare: ">. Try again.`

`split_commas` (L480-485): split on `,`, **remove every space** in each part, drop empties.

1. **Scope** (no --org, no --repo). Choices: in a GitHub checkout
   `Repositories you list (one or more), starting with <here>` and `Everything one GitHub owner owns: <owner of here>`; else `Everything one GitHub owner owns` and `Repositories you list (one or more)`. step `What the team works on` / `Its agents may point each other only at these repositories, on any branch. Anything else reaches them as outside content, marked so.`;
   `gum choose --header Trust --selected <choice 1> <choice 1> <choice 2>`; non-zero or empty ->
   cancelled; answered. Then:
   - owner choice: step `GitHub owner` / `The organisation or user the team works for: all of its repositories, never forks.`; ask_valid check_owner `GitHub owner (organisation or user)`
     `e.g. example-org` `<owner of here>`; lower-cased; answered.
     `check_owner` (L505-515): lower-case; `declare check --file F --org <o>` (refusal ->
     say_problem); `gum spin --title "Looking up <o> on GitHub…" -- gh api users/<o> --silent`;
     failure -> say_problem `GitHub has no user or organisation called '<o>' that your gh login can see`.
   - repos choice: step `Repositories` / `owner/repo, comma-separated: one, or up to 32 of any owners, each one your gh login can push to.`; ask_valid check_repos `Repositories (owner/repo, comma-separated)` `e.g. example-org/alpha` `<here>`; repos = split_commas(lower(answer));
     answered (space-joined).
     `check_repos <list> [noninteractive]` (L551-584): per entry, lower-cased: `declare check --file F --repo <r>` refused -> `  <r>: not OWNER/REPO (lower-case; the name not ., .. or ending .git).`;
     else `  Checking <r> on GitHub…`, `gh api repos/<r> --jq .permissions.push`: failure -> `  <r>: not found, or your gh login cannot see it.`; output not `true` -> `  <r>: read-only for your gh login; a team trusts only repositories you can push to.` No entries -> say_problem `name at least one repository, as owner/repo`. Any bad -> `  Try again.` (interactive only), refuse. Then
     the whole list `declare check --file F --repo a --repo b...` (refusal -> say_problem).
2. **Team name** (no --team): default = `declare suggest-team --file F --name <owner>` or
   `--name <repo part of repos[0]>` (may be empty). step `Team name` / `Used in the room's name and its agents' names: a-z, 0-9 and -, up to 24.`; ask_valid check_team `Team name` `e.g. example-org`
   `<default>`. `check_team`: empty -> say_problem `the team needs a name`; else `declare check --file F --team <t>`.
3. **People** (no --human): login = `id -un`; `declare check --file F --human <login>` refused ->
   `Your login name, <login>, cannot be an account name here (<reason minus "declare: ">), so there is no default.` and no default. step `People` / `Each gets an Element login to the room; the installer creates the accounts. Add others after a comma.`; ask_valid check_humans `People, comma-separated`
   `e.g. <login or yourname>, colleague` `<login>`. check_humans: none -> say_problem `name at least one person; your own login name is the usual answer`; else `declare check --human ...`.
4. **Other machines** (no --allow-from): step `Other machines` / `Usually none: ccy sessions are on this machine. Only for LXC containers or other machines running agents, their subnet.`; ask_valid
   check_subnets `Subnets (Enter for none)` `e.g. 192.0.2.0/24` \`\`; empty accepted; else `declare check --allow-from ...`. answered `<list>` or `none, this machine only`.
5. **Bus address** (none in the host_vars): step `Bus address` / `Every team's room lives at one address of this machine, used for nothing else: an IP its agents and your Element can reach, such as its WireGuard address, or one added for the bus.`; ask_valid check_bus_address `This machine's bus address` `e.g. 192.0.2.10` \`\`; check = `declare check-address --address <a>`. Adds
   `--address <a>` to create's flags.

#### 2.12.4 --yes checks (L706-714)

With `--yes` and repos: need_gh; `check_repos <comma list> noninteractive`; failure -> `ERROR: create stopped: fix the repositories above (or drop them); nothing was written.` exit 1. (`--org` is not
looked up on GitHub in either mode when given as a flag: oddity 8.14.)

#### 2.12.5 Summary and write (L715-741)

Flags: `--team`, each `--human`, `--org`, each `--repo`, each `--allow-from`, plus `--address`.
Interactive only: `declare create --file F <flags> --dry-run` (stdout discarded; failure exits with
its code). trusts: org -> `everything <org> owns: any repository, any branch`; one repo -> `only <repo>, on any branch`; several -> `only these repositories, on any branch: <a>, <b>`.
`summary_confirm` (L807-819): `gum style --border rounded --padding "0 1" "Team <team>" "" <body>`
where body is `printf '%-9s %s\n'` of `Works on`/trusts, `People`/humans space-joined,
`Machines`/allow-from or `this one only`, `Element`/`a profile for the team, set up on this desktop`;
then `gum confirm --default=true "Create team <team>? (saved to <HOST_VARS ~>, then installed: sudo asks for your password)"`: 1 -> `Nothing was added.` exit 1; other non-zero -> cancelled.
Then `declare create --file F <flags>` (stdout `CHANGED\tteam <t>, port <p>, in <file>` passes to
our stdout; stderr `Team ... kept at <backup>.`), `declare element --file F --team <t>` (stdout
discarded).

#### 2.12.6 Install (L745-748)

`apply_teams <team>` (2.13.2); failure -> `ERROR: team <t> is declared in <HOST_VARS ~> but not installed. Once the cause above is fixed, ccy-teams apply <t> installs it.` exit 1.

#### 2.12.7 After (L749-751)

Not --yes: `after_create` (L756-768): per human `ask_yes "Sign <name> in to Element now? A password is set and shown here once." true` -> `new_password` (failure ignored); then `ask_yes "Open <team> in Element now?" true` -> launch_element (failure ignored). Always: `declare next-steps --file F --team <t>` to **stderr** (DP L733-742 text). Not --yes: `offer_join <team>`.

#### 2.12.8 `ask_yes <q> <default>` (L957-964)

`gum confirm --default=<true|false> <q>`: 0 yes, 1 no, other -> cancelled (exit 130).

#### 2.12.9 `offer_join <team>` (L781-792)

In a work tree, the team among `join_candidates`: top = toplevel; `ask_yes "Start a ccy session in <top ~> as a member of <team>?" true`; yes -> `Starting ccy --teams <team> in <top ~>.`; chdir top;
exec `<CCY_LAUNCHER> --teams <team>`.

### 2.13 apply

#### 2.13.1 `cmd_apply` (L916-954)

need_checkout; exactly one arg else usage `'apply' takes one team, or --all.`; `refresh || exit 1`.

- `--all`: outdated/unbuilt -> targets; decide -> waiting `<team> (<status_word>)`; unreadable ->
  broken `<team>: <detail>`. Broken -> `ERROR: these teams' entries cannot be read, so nothing was applied: <list>.` exit 1. Waiting -> `Left alone, as they need your decision (ccy-teams remove <team>, or declare them again): <list space-joined>.` No targets -> `No team is out of date or waiting to be built.` exit 0. Targets sorted.
- `<team>`: outdated/unbuilt/current -> target; decide/removed -> usage `team <t> is not declared present; ccy-teams remove <t> takes a team down.`; unreadable -> usage `team <t>'s entry cannot be read: <detail>.`; unknown -> usage `team <t> is not declared in <HOST_VARS ~>.`
- `apply_teams targets`; failure -> `ERROR: not applied: <targets>. Their entries stay as declared; once the cause above is fixed, ccy-teams apply runs the play again.` exit 1.

#### 2.13.2 `apply_teams <team>...` (L887-911)

1. `run_play` (L878-883): JSON = Python `json.dumps({"agent_bus_only_teams": [...]})`, i.e. exactly
   `{"agent_bus_only_teams": ["a", "b"]}` (`": "` and `", "` separators; TC asserts the literal).
   say `Running play-agent-bus.yml for <teams>: it starts each team's homeserver, and creates its room and its people's accounts.`; run `<PLAY> -e <json>` (the play file is executable: its
   shebang is run.bash's single-play mode). Output streamed.
2. Non-zero -> `ERROR: play-agent-bus.yml failed (exit <rc>, above).` return 1.
3. `refresh`; any team not `current` -> `ERROR: the play finished, but these teams are still not as declared: <team (word)> ....` return 1.
4. `element_teams` (L340-348): `declare element-teams --file F` (`ELEMENT\t<team>\t<url>`), those
   named. Non-empty -> run_element_play; failure -> `ERROR: the homeserver side is applied, but the Element profile is not; once the cause above is fixed, ccy-teams apply <teams> runs both again.`
   return 1.
5. say `Applied: <teams>.`

### 2.14 remove (`cmd_remove`, L969-1033)

1. need_checkout; exactly one arg not starting `-`, else usage `'remove' takes one team (and no --yes: it always asks).`; stdin and stderr TTYs else usage `'remove' asks on a terminal, always: taking a team down is a human's decision.`; need_gum; refresh || exit 1.
2. state empty/removed/unbuilt -> usage `team <t> is not built here; there is nothing to take down.`;
   unreadable -> usage `team <t>'s entry cannot be read: <detail>.`
3. `gum style --bold --border rounded --padding "0 1" "Take team <t> down"`; explain `Its homeserver and its backup timer stop: its agents and people can no longer reach it. Its data (the room's whole history, the accounts, the backups) stays on this machine unless you choose to delete it, which cannot be undone. There may be history in it worth keeping.`
4. `ask_yes "Back <t> up first?" true`: yes -> sudo_ready `A backup is taken as root, by agent-bus-install.` (fail exit 1); `sudo /usr/local/sbin/agent-bus-install backup-now --team <t>`
   failure -> `ERROR: the backup failed (above), so <t> was not taken down.` exit 1.
5. `gum choose --header "What happens to <t>?" "Take it down, keep its data" "Take it down and delete its data" "Leave it running"` (non-zero -> cancelled). Delete: `gum input --header "Type <t> to delete its data for good" --placeholder "the team's name" --value "" --width 0` (cancel ->
   cancelled); not equal -> `That is not the team's name; nothing was changed.` exit 1; purge. Leave
   (or anything else) -> `Nothing was changed.` exit 1.
6. DETAIL_OF `undeclared`: sudo_ready `A team that is not declared is taken down as root, by agent-bus-install.`; `sudo /usr/local/sbin/agent-bus-install remove --team <t> [--purge]`, failure
   `ERROR: <t> was not taken down (above).` exit 1. Otherwise: `declare remove --file F --team <t> [--purge]` (stdout `CHANGED ...` to our stdout), then `run_play <t>` failure -> `ERROR: <t> is declared absent in <HOST_VARS ~> but is still running; once the cause above is fixed, ccy-teams remove <t> runs the play again.` exit 1.
7. refresh; status not empty and not `removed` -> `ERROR: <t> is still <word> after the play.` exit 1.
8. say `Team <t> is down and its data deleted.` or `Team <t> is down; its data is kept in /var/lib/agent-bus/<t>.`

### 2.15 backup (`cmd_backup`, L1037-1041)

Exactly one arg not starting `-` else usage `'backup' takes one team.`; sudo_ready `A backup is taken as root, by agent-bus-install.` (fail exit 1); `sudo /usr/local/sbin/agent-bus-install backup-now --team <t>` (status is the exit status).

### 2.16 restore (`cmd_restore`, L1058-1108)

Args: `--yes`; other `-*` -> usage `unknown restore option '<a>'.`; first positional team, second id,
third -> usage `unexpected argument '<a>'.` No team -> usage `'restore' needs a team.` id not
`^[1-9][0-9]*$` -> usage `a backup is named by its number; got '<id>'.` sudo_ready `The backups are listed and restored as root, by agent-bus-install.` (fail exit 1).

- No id: listing = `sudo /usr/local/sbin/agent-bus-install backups --team <t>` (lines
  `BACKUP\t<id>\t<time>`). Empty -> `ERROR: team <t> has no backup to restore.` exit 1. stdin or
  stdout not a TTY -> listing to **stdout**, then usage `name the backup to restore: ccy-teams restore <t> <n> --yes.` (64). Else `pick_backup` (L1045-1056): rows `Backup %-6s %s` (id, time);
  `ccy_tmux_pick "Restore <t>" "$(ccy_tmux_header "Enter choose   Esc or q exit" "The homeserver is stopped, the backup's database and state put back, and it is started again.")" ""`; cancelled ->
  `Nothing restored.` exit 0.
- Not --yes: stdin or stdout not a TTY -> usage `a restore replaces the team's database and state: confirm it with --yes when there is no terminal.`; `ccy_tmux_confirm "Restore <t>" "Restore backup <id> of <t>? Its homeserver stops, and every message and change since that backup is lost." "restore backup <id>"`; no -> `Nothing restored.` exit 0.
- `sudo /usr/local/sbin/agent-bus-install restore --team <t> --backup <id>`.

### 2.17 The shared picker (TS L780-845) — exact behaviour

`ccy_tmux_header <keys> [lines...]`: `↑↓ choose   <keys>` then each non-empty line on its own line.

`ccy_tmux_pick <title> <header> <expect> [cursor]`: fzf missing -> `ERROR: fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.` return 1. Input = rows (trailing newline
stripped) + a final row `Exit`. argv, **in this order** (TC parses it):

```
fzf --ansi --height=~70% --layout=reverse --no-multi --no-sort --no-info
    --color=16,fg:-1,bg:-1,fg+:-1:bold,bg+:-1,hl:3,hl+:3:bold,gutter:-1,pointer:3,marker:3,prompt:3,spinner:3,info:-1,header:-1,border:-1,label:3:bold
    --border=rounded --border-label=" <title> " --border-label-pos=3
    --margin=1,2 --padding=1,2 --header-first --pointer=▶
    --prompt="filter > " --header="<header>\n" --bind=q:abort
    [--expect=<keys>] [--bind=start:last]
```

Non-zero fzf exit returned as is. No expect keys: output is prefixed with an empty key line. Chosen
row `Exit` -> return 1. Prints `<key>\n<row>`.

`ccy_tmux_confirm <title> <question> <yes-label> [yes]`: `ccy_tmux_pick <title> "$(ccy_tmux_header "Enter choose   Esc or q exit" <question>)" "" <last unless 4th is yes>` over the
single row `Yes, <yes-label>`; true only if the chosen row starts `Yes, `.

## 3. External interfaces

### 3.1 Environment variables honoured

| var                      | default                                                               | used for                                            |
| ------------------------ | --------------------------------------------------------------------- | --------------------------------------------------- |
| `CCY_LIB`                | `/var/local/claude-yolo/lib`                                          | install guard (L97, L142)                           |
| `CCY_TEAMS_HOST_VARS`    | `<ROOT>/environment/localhost/host_vars/localhost.yml`                | every declare call                                  |
| `CCY_TEAMS_LAUNCHER`     | `/var/local/claude-yolo/claude-yolo`                                  | exec for new sessions                               |
| `CCY_TEAMS_PLAY`         | `<ROOT>/playbooks/imports/play-agent-bus.yml`                         | apply/remove                                        |
| `CCY_TEAMS_ELEMENT_PLAY` | `<ROOT>/playbooks/imports/optional/common/play-agent-bus-element.yml` | Element                                             |
| `CCY_TEAMS_APPLIED_DIR`  | `/var/lib/agent-bus-applied`                                          | stamps                                              |
| `CCY_STATE_DIR`          | unset                                                                 | registry base (SR L56)                              |
| `XDG_STATE_HOME`         | `$HOME/.local/state`                                                  | declined dir, element log, registry                 |
| `HOME`                   |                                                                       | `~` abbreviation, launcher path, restore unit check |
| `NO_COLOR`               |                                                                       | colour off                                          |
| `WAYLAND_DISPLAY`        |                                                                       | clipboard offer                                     |
| `PATH`                   |                                                                       | every tool is found on PATH (fakes)                 |
| `PWD`                    |                                                                       | `here`                                              |
| GUM\_\* (set, not read)  |                                                                       | see need_gum                                        |

### 3.2 Commands run (argv shapes)

| command                                                                                                                                                                            | where                                   | stdout/notes                                                                                                                                              |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `env PYTHONPATH=<ROOT> python3 -P -m helpers.agent_bus.declare <sub> ...`                                                                                                          | anywhere                                | see 3.3                                                                                                                                                   |
| `git rev-parse --is-inside-work-tree` (stdout `true`), `git rev-parse --show-toplevel`, `git remote get-url origin`                                                                | cwd                                     |                                                                                                                                                           |
| `systemctl list-units --all --plain --no-legend --type=service agent-bus-hs@*`                                                                                                     |                                         | `<unit> <load> <active> <sub> <desc>`                                                                                                                     |
| `systemctl is-active agent-bus-hs@<t>.service`                                                                                                                                     |                                         | `active` exit 0                                                                                                                                           |
| `tmux -L ccy list-sessions -F '#{session_name} #{session_attached} #{session_path}'`                                                                                               |                                         |                                                                                                                                                           |
| `tmux -L ccy list-clients -t =<s> -F '#{client_tty} #{client_activity}'`, `tmux -L ccy set-hook -g client-attached <hook>`, `tmux -L ccy attach-session -t =<s>`                   |                                         | attach                                                                                                                                                    |
| `fzf ...` (2.17), `gum input/confirm/choose/style/spin` (2.12)                                                                                                                     |                                         |                                                                                                                                                           |
| `sudo -n -v`, `sudo -v`                                                                                                                                                            |                                         | sudo_ready                                                                                                                                                |
| `sudo -n /usr/local/bin/agent-bus list <t>`                                                                                                                                        |                                         | `HUMAN\t<name>\t<membership or ->`, `MEMBER\t<handle>\t<role>\t<membership or ->\t<active\|parked>` (helpers/agent_bus/admin.py L791-801)                 |
| `sudo -n /usr/local/bin/agent-bus human devices <t> <name>`                                                                                                                        |                                         | `DEVICE\t<id>\t<ms or ->` (admin.py L839-855)                                                                                                             |
| `sudo -n /usr/local/bin/agent-bus human password <t> <name>`                                                                                                                       |                                         | one line: the password (cli.py L133-137)                                                                                                                  |
| `sudo -n /usr/local/bin/agent-bus park-member <t> <handle>`                                                                                                                        |                                         | `CHANGED\t...` lines (inherit our stdout)                                                                                                                 |
| `agent-bus seat list` (no sudo)                                                                                                                                                    | cwd = checkout                          | `SEAT\t<team>\t<seat>\t<held\|free>\t<self\|->\t<handle>`; exit 78 when a seat is unreadable but others still printed (cli.py L221-227; seat.py L240-254) |
| `agent-bus seat remove <seat>@<team>` (no sudo)                                                                                                                                    | cwd = checkout                          |                                                                                                                                                           |
| `sudo /usr/local/sbin/agent-bus-install backup-now --team <t>` / `backups --team <t>` (`BACKUP\t<id>\t<time>`) / `restore --team <t> --backup <n>` / `remove --team <t> [--purge]` |                                         |                                                                                                                                                           |
| `<PLAY> -e '{"agent_bus_only_teams": [...]}'`                                                                                                                                      |                                         | streamed                                                                                                                                                  |
| `<ELEMENT_PLAY>` (no args)                                                                                                                                                         |                                         | streamed                                                                                                                                                  |
| `systemd-run --user --scope --quiet --collect --unit ccy-teams-element-<t>-<pid> --description "Element for agent team <t>" -- setsid -w gio launch <desktop>`                     |                                         | log file                                                                                                                                                  |
| `wl-copy` (password on stdin)                                                                                                                                                      |                                         |                                                                                                                                                           |
| `gh auth status`, `gh api users/<o> --silent` (inside gum spin), `gh api repos/<r> --jq .permissions.push`                                                                         |                                         |                                                                                                                                                           |
| `id -un`                                                                                                                                                                           |                                         |                                                                                                                                                           |
| `tput cols`                                                                                                                                                                        |                                         | width                                                                                                                                                     |
| `<CCY_LAUNCHER> --teams <ref>`                                                                                                                                                     | exec, cwd = checkout top                |                                                                                                                                                           |
| `ss -Hltn`                                                                                                                                                                         | run by declare.py create, not ccy-teams |                                                                                                                                                           |

The agent-bus wrapper's exit codes: 0, 64, 69 unreachable, 70 refused, 75 busy, 77 not root / seat
run as root, 78 config (cli.py L13-16; wrapper L26-27). agent-bus-install: 0, 1, 64, 77, 78.

### 3.3 declare.py subcommands used (DP; exit 0 ok, 64 usage, 78 refused, 130 Ctrl-C; errors

`declare: <msg>` on stderr)

| subcommand      | args                                                                                      | stdout                                                                |
| --------------- | ----------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `declared`      | `--file F`                                                                                | `TEAM\t<team>\t<port\|->\t<present\|absent>\t<server_name>\t<url\|->` |
| `status`        | `--file F --source <ROOT> --applied <dir> [--installed T]...`                             | `STATUS\t<team>\t<state>\t<detail\|->`                                |
| `limited`       | `--file F`                                                                                | `LIMITED\t<team>\t<p1, p2>\t<scope words>`                            |
| `drop-limit`    | `--file F --team T`                                                                       | `CHANGED\t...` (+ backup note on stderr)                              |
| `teams-for`     | `--file F --repo O/R`                                                                     | `TEAM\t<team>`                                                        |
| `github-remote` | `--url U`                                                                                 | `owner/repo` or nothing                                               |
| `address`       | `--file F`                                                                                | the bus address or nothing                                            |
| `check-address` | `--address A`                                                                             | nothing; 64 with reason                                               |
| `check`         | `--file F [--team] [--human]... [--org] [--repo]... [--allow-from]...`                    | nothing; 64 with reason                                               |
| `suggest-team`  | `--file F --name N`                                                                       | a name or nothing                                                     |
| `create`        | `--file F --team --human... (--org\|--repo...) [--allow-from]... [--address] [--dry-run]` | `CHANGED\tteam T, port P, in F`; dry run: the entry's lines           |
| `element`       | `--file F --team T`                                                                       | `CHANGED\t...` or `UNCHANGED\tT`                                      |
| `element-teams` | `--file F`                                                                                | `ELEMENT\t<team>\t<base_url>`                                         |
| `next-steps`    | `--file F --team T`                                                                       | the next-steps text (ccy-teams sends it to stderr)                    |
| `remove`        | `--file F --team T [--purge]`                                                             | `CHANGED\tteam T absent, ...`                                         |

Recommendation: the Go port keeps calling declare.py (it owns the host_vars line-editing, the team
file validator and the D65 digests, which `ccy_teams_play` in the tests also uses). Re-implementing
it is out of scope for behaviour parity.

### 3.4 Files

| path                                                                           | access                                                                         | format                                                                                                                                                                                                                                      |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HOST_VARS                                                                      | via declare.py only (read, and written with a `.backup.<UTC stamp>` beside it) | YAML subset                                                                                                                                                                                                                                 |
| `<APPLIED_DIR>/*.json`                                                         | names globbed (L1193); content read by declare                                 | `{"v":1,"team","config","software"}`                                                                                                                                                                                                        |
| registry `<base>/sessions/<name>`                                              | read; rewritten by set_restore (temp `<name>.tmp.<pid>`, umask 077, `mv -f`)   | line 1 `ccy-session-record 1`, then `name=`, `dir=`, `launcher=`, `prefix=`, `restore=yes\|no`, optional `compose=started\|declined\|running`, `arg=` repeated in order; unknown key, missing field or bad value = unreadable (SR L532-594) |
| `<dir>/.claude/ccy/pingbus/seats`                                              | existence test only                                                            |                                                                                                                                                                                                                                             |
| `<DECLINED_DIR>/path-limit-<team>`                                             | created empty                                                                  | marker                                                                                                                                                                                                                                      |
| `<state>/ccy-teams/element-<team>.log`                                         | appended                                                                       | Element's output                                                                                                                                                                                                                            |
| `$HOME/.local/share/applications/agent-bus-<team>-element.desktop`             | existence; passed to gio                                                       |                                                                                                                                                                                                                                             |
| `$HOME/.config/systemd/user/default.target.wants/ccy-sessions-restore.service` | existence                                                                      |                                                                                                                                                                                                                                             |
| `/dev/tty`                                                                     | read one line                                                                  | back_to_list                                                                                                                                                                                                                                |
| temp file (mktemp)                                                             | as_root's stderr                                                               | removed                                                                                                                                                                                                                                     |

## 4. Data model

- **Team** (name `[a-z][a-z0-9-]{0,23}`): union of (a) host_vars entries with state present
  (`declare declared`), (b) loaded `agent-bus-hs@<team>.service` units, (c) applied stamps. Fields:
  port, server name (default `<team>.agent-bus.internal`), URL (`http://<first listen or bus address>:<port>`, IPv6 bracketed), LOADED (has a unit).
- **Homeserver state** (STATE column of a team row): `up`, `down (<systemd word>)`, `not installed`
  (no unit).
- **Applied state** (APPLIED column, 2.4): from declare status, comparing the stamp's config digest
  (team file rendered from the entry) and software digest (admin zipapp + pinned Tuwunel + installer
  hash) with the checkout.
- **Member** (`agent-bus list`): handle `<repo>.<seat>+<host>.<type>` (type podman, lxc, docker, vm,
  host), role, room membership, active/parked. KIND = type with podman shown as `ccy`.
- **Seat** (`agent-bus seat list` per checkout): team, seat (`[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}`),
  held/free, self/-, handle. Checkouts considered: dirs of live `ccy-*` tmux sessions, dirs of `ccy`
  registry records, and the cwd's work tree.
- **Session**: a tmux session on socket `ccy` (name, attached count, path) and/or a registry record
  (name, dir, launcher, prefix, restore, args). Seat \<-> session via the record's `--teams` list
  entries keyed `<dir>|<seat>@<team>`. SESSION column: session name, `name(stopped)` (record but no
  tmux session), or the seat state when no record names it. RESTORE: record's yes/no, else `-`.
- **Human** (`HUMAN` lines): name, membership (`in room`/`invited`/`not in room`), signed-in
  (`never signed in`, `<n> signed in`, `need sudo`, `unknown`) from `human devices`.
- **Reasons members are missing** (MEMBERS_UNKNOWN): `not installed`, `homeserver down`, `need sudo`,
  `read failed (above)`. INCOMPLETE: `a seat is unreadable` / `a session record is unreadable`.

## 5. User-facing strings the suite asserts on (keep verbatim)

Strings are substrings unless marked "exact". Grouped by flow; TC line in brackets.

- **usage**: `ccy-teams -l|--list` [500]; absence of `set -euo` [501].
- **--list**: heading contains `TEAM`, `RESTORE`, `APPLIED` on line 1 [512-515]; team row regex
  `^alpha +- +team` with `up`, `port 6167`, `up to date` [516-519]; `^alpha +1 +ccy` with `active`,
  `ccy-proj`, `yes`, the checkout path [520-524]; `free` [525]; `parked` on the
  `proj\.2\+local\.podman +ccy` row [526]; seat handle not listed twice [528]; lxc row `active` [529];
  human `in room`, `never signed in`, `Enter: new password` [531-533]; `2 signed in` [547];
  `down (inactive)` and exact fragment `port unknown (not declared), members: homeserver down`
  [535-536]; `not installed`, `ccy-teams apply alpha` [553-554]; never `meta-deploy` [555]; colour
  `\e[32m` on a TTY, none with NO_COLOR or piped [542, 560-562]; no-sudo: exit 1, `members of alpha are not shown`, `play-agent-bus.yml`, `need sudo`, `members: need sudo` [567-571]; with the rule, nothing
  on stderr containing `sudo` [578]; no teams: exit 0 and `ccy-teams create` [593-594]; refused
  host_vars shape: exit 1 [598]; unreadable seat: other seats listed, team row `a seat is unreadable`
  [603-606]; out of date `out of date: config`, `not built`, `decide: absent`, `decide: undeclared`
  [1161-1166].
- **picker/sudo**: needs a terminal exit 1 and `--list` [618-619]; Esc -> `rc=0` [623]; READ_WHY
  contains `done as root` and `play-agent-bus.yml` [635-636]; `sudo was not given your password`,
  `members cannot be shown`, never `No agent teams`, zero fzf calls [643-647].
- **attach**: `tmux attach-session -t =ccy-proj` logged [661]; `Ctrl-T` [668].
- **restore toggle**: record rewritten `restore=no`, args kept `--teams 1@alpha`, next list `no`
  [673-679]; `has no session` [683].
- **remove/park**: `agent-bus seat remove 1@alpha (in proj)` [687]; park argv
  `sudo -n /usr/local/bin/agent-bus park-member alpha proj.box+role.lxc` [693]; `could not be read`
  [609]; team removal shows `history` [703].
- **Ctrl-N**: `not a project folder` [708]; rows exact `Seat dev@alpha   free`, `Seat 3@alpha   new`
  [713]; `launcher --teams 3@alpha`, `--teams dev@alpha`, `(in proj)` from a subdirectory [714-725];
  `homeserver is down` [720]; `Not every seat` [612].
- **Element**: `has no Element profile on this desktop yet` [735], never `agent_bus_element_teams`
  [736]; `base_url: "http://192.0.2.10:6167"` written [743]; `play-agent-bus-element.yml failed`
  [751]; `gio launch <desktop>` [746, 757]; `systemd-run --user --scope --quiet --collect --unit ccy-teams-element-alpha-` [761]; no `electron chatter` on the terminal; `ccy-teams/element-alpha.log`
  [762-765]; `Element did not start` and the log's tail [770-772]; never-signed-in password offer
  [776-782].
- **password**: confirm header contains `other Element sessions are signed out` [789];
  `http://192.0.2.10:6167`, `Username`, `@owner:alpha.agent-bus.internal`, the password [791-794];
  clipboard via stdin [801-803]; `Yes, copy the password`, with no `start:last` in that call, while
  the reset's confirm has it [805-808]; `not on the clipboard`, never `The password is on the clipboard.` on failure [811-812]; `No password was set for owner.` [817]; `KIND human` [820].
- **help**: `Ctrl-U`, `decide: absent`, `need sudo`, `Colours:` [825-830].
- **backup/restore**: sudo argv `sudo /usr/local/sbin/agent-bus-install backup-now --team alpha`
  [836], `restore --team alpha --backup 2` [841]; listing on stdout contains `BACKUP\t2` [848];
  picker row text `Backup 1` [852].
- **create --yes**: stdout `CHANGED\tteam beta, port 6169` [869]; play argv exact
  `play -e {"agent_bus_only_teams": ["beta"]}` [870]; `ccy --teams beta` [871]; no `localhost.yml` on
  stderr except lines containing `kept at` [873]; order play then element-profiles [876]; repo errors
  `example-org/readonly: read-only`, `other/missing: not found`, `bad: not OWNER/REPO`, not
  `example-org/proj:` [895-898]; `ccy-teams apply beta` on failures [906, 915]; exit 78 with no bus
  address and `ccy-teams create`, never `localhost.yml` [1029-1031].
- **no teams / wizard**: `No agent teams on this machine yet`, `ccy-teams create` [940-941]; gum log
  exact `gum confirm --default=false Create one now?` [943]; `gum choose --header Trust --selected Repositories you list (one or more), starting with example-org/proj` [961]; `--value example-org/proj` on the first input, `--header Team name` with `--value proj`, `--header People`
  with `--value owner` [963-967]; `gum style --width` carrying `installer creates the accounts`
  [968]; no `gum input|filter` header of 70+ chars [970]; `gum confirm --default=true Create team proj?` [972]; summary `only example-org/proj, on any branch`, `a profile for the team, set up on this desktop` [973-974]; `ccy --teams proj` [975]; no `localhost.yml` except `saved to`/`kept at`
  lines [977]; `gum confirm --default=true Sign owner in to Element now?`, `gum confirm --default=true Open proj in Element now?` [978-979]; `launcher --teams proj (in proj)` [981]; GUM env
  [983-988]; `1/5  What the team works on`, `5/5  Other machines` [989-990]; `✓` line with the repo
  [991]; never `forge_api`, `path_prefixes` [992-993]; `private room` exactly once [994]; owner scope
  `everything example-org owns: any repository, any branch` [1012]; bus address `not an IP address`
  [1020]; re-prompt `--value Example-Org/Alpha, example-org/readonly, other/missing, bad` (raw typed
  text, not lower-cased) [1042]; `only these repositories, on any branch: example-org/alpha, other/tool` [1047]; `Try again`, `no user or organisation called 'nope'` [1242-1243]; `Cancelled; nothing was changed.` with 130 [1250-1255]; no `Traceback` [1256]; `Nothing was added.` [1261];
  `gh auth login` [1267]; `play-claude-yolo.yml` when gum missing [1271].
- **path limit**: `gum confirm --default=true Remove the limit, so any path in its repositories can be named?` [1065]; `may only point each other at files under`, `Team legacy (for any repository of example-org)`, `a team already on this machine`, never `was the default` [1066-1069]; marker means
  never asked again [1091].
- **checkout no team covers**: `No team on this machine works on example-org/proj (this checkout)`,
  `The teams listed next are for other repositories`, `gum confirm --default=true Create a team for example-org/proj now?`, `ccy-teams create starts one whenever you like` [1109-1112]; nothing about
  `this checkout` outside one [1116].
- **update offer**: `Press Enter to go back to the list.` [1124]; `Not as declared` [1226].
- **apply/remove**: one play run with exact `"agent_bus_only_teams": ["beta", "delta"]` [1173];
  `gone (decide: absent)`, `zeta (decide: undeclared)`, `Applied: beta delta.` [1174-1176]; never
  `agent-bus-install remove` from apply [1177]; `ccy-teams remove gone` [1190]; `gum confirm --default=true Back beta up first?` [1202]; `purge: false` / `purge: true` [1203, 1216]; `its data is kept` [1205]; `sudo /usr/local/sbin/agent-bus-install remove --team zeta` without `--purge`
  [1220-1221].

## 6. Test seams (driving a Go binary with TC unchanged)

TC puts fakes first on PATH (`$BIN`, `$GUMBIN`) and sets: `HOME`, `CCY_STATE_DIR`, `CCY_LIB`,
`CCY_TEAMS_HOST_VARS`, `CCY_TEAMS_LAUNCHER`, `CCY_TEAMS_PLAY`, `CCY_TEAMS_APPLIED_DIR`,
`CCY_TEAMS_ELEMENT_PLAY`, `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CEILING_DIRECTORIES`, unsets `NO_COLOR WAYLAND_DISPLAY XDG_STATE_HOME`; per-case `NO_COLOR`,
`WAYLAND_DISPLAY`. Fake-control vars (read by fakes, not by ccy-teams): `TEST_LOG`, `TEST_STATE`,
`TEST_ANSWERS`, `TEST_FZF_ROWS`, `TEST_FZF_RAW`, `TEST_GUM_ANSWERS`, `TEST_SUDO_RULE`,
`TEST_SUDO_REFUSE`, `TEST_GIO_FAIL`, `TEST_WLCOPY_FAIL`, `TEST_GH_NOAUTH`, `TEST_PLAY_FAIL`,
`TEST_ELEMENT_FAIL`, `TEST_REPO_ROOT`. Fakes: tmux, fzf, systemctl, sudo, agent-bus,
agent-bus-install, ss, gio, systemd-run, wl-copy, launcher, id, gh, play, element-play, gum. Real:
git, python3 (declare.py and `tests/helpers/agent_bus/ccy_teams_play.py`), the bash registry library
(TC sources it to write and read records: the Go registry code must be format-compatible).
Terminals come from `script -qec` (pty on 0/1/2; stdin of script is /dev/null, so /dev/tty reads hit
EOF).

**The Go binary must therefore**: find every tool via PATH (no absolute paths except the two sudo
targets, which go through the fake sudo by basename); never call a tool the fakes reject (the tmux
fake allows only list-sessions/list-clients/set-hook/attach-session; systemctl only
list-units/is-active; gh only the shapes in TC L275-286; agent-bus only seat list/seat remove/list/
park-member/human devices/human password; agent-bus-install only backups/backup-now/restore/remove);
pass `-L ccy` first to tmux; run `agent-bus seat ...` with cwd set to the checkout (the fake logs
`(in <basename>)`); exec the launcher with cwd = checkout top.

**Dependencies on bash internals rather than behaviour (flag; TC must change for Go):**

01. Template rendering and Jinja-free check (TC L67-83) and `shellcheck` of the rendered script (L84-85):
    meaningless for Go. Replace with: build the binary with ROOT_DIR injected; `go vet`/tests.
02. TC `TOOL` is a script path; for Go it becomes the built binary path (with ROOT_DIR injected).
03. The fzf fake reconstructs calls from `"$*"` and the exact flag set: `--ansi` (decides colour
    stripping), `--expect` presence (decides whether a key line is printed), the `Exit` row, `Yes, `
    rows, and the awk window `/Copy the password\?/ ... /--bind=q:abort/` (TC L804, L808) which needs
    `--border-label` before `--header`, the header text inside argv, and `--bind=q:abort` followed on
    the same printed line by `--expect`/`--bind=start:last`. This is a dependency on argv *order*.
04. The gum fake parses `--value`, `--selected`, `--`, and treats `--header --placeholder --title --border --padding --width` as taking a value; other `--x` flags take none (so `--default=true`
    must be one word). Assertions grep the *log line* `gum <sub> <args joined by space>`, so argument
    order is asserted (e.g. `gum confirm --default=true <question>`, `gum choose --header Trust --selected <x> ...`, `gum style --width`). Also it snapshots the GUM\_\* environment.
05. The sudo fake decides "password needed" by its own rule file and prints `sudo: ...`; the as_root
    heuristic (exit 1 + stderr starting `sudo: `) must be kept exactly.
06. `check_lacks "command not found"` (TC L1125) guards a bash-only failure (function defined after
    use); harmless for Go.
07. `back_to_list` reads `/dev/tty`; the suite relies on that read failing at EOF and the run going
    on. A Go implementation reading os.Stdin instead would also hit EOF here, but must use /dev/tty
    for parity with real use after fzf.
08. The `script` wrapper turns `\n` into `\r\n`; TC strips `\r`. Nothing for Go to do.
09. The play fake requires `-e` as argv[1] and the JSON as argv[2] (TC L295-297).
10. `TEST_FZF_ROWS`/`R_TEXT` equality: TC checks rows by regex on columns (`^alpha +1 +ccy`), so the
    exact column widths matter.
11. TC L1089 relies on the declined marker living under `$HOME/.local/state` (XDG_STATE_HOME unset)
    and surviving between runs.

## 7. Shared code for phase 2 (ccy-sessions) and phase 3 (ccy): proposed `internal/` packages

`src-go/` already exists (module `github.com/LongTermSupport/fedora-desktop/src-go`, go 1.26) with
`internal/execx` (tests only so far); build on it.

| package             | bash it replaces                                                                                                                                                                                                                                                                                                                                              | used by                                                            |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/term`     | `colour_on`, `say`, `print_error` (CP L25), `answered`, `step`, `~` abbreviation                                                                                                                                                                                                                                                                              | all three                                                          |
| `internal/tmuxccy`  | `ccy_tmux` (`-L ccy`), `ccy_tmux_list`, `_ccy_tmux_clients`, `ccy_tmux_is_detached`, `ccy_tmux_single_attach_hook`, `ccy_tmux_attach` (TS L36-140); later `ccy_tmux_take_over`, `ccy_tmux_other_terminals`, `ccy_tmux_idle_words`, `ccy_tmux_client_words`, `ccy_tmux_next_name`, `ccy_tmux_project_sessions`, `ccy_tmux_insulate`, `ccy_tmux_start_detached` | teams, sessions, ccy                                               |
| `internal/picker`   | `ccy_tmux_pick`, `ccy_tmux_confirm`, `ccy_tmux_header` (TS L780-845): fzf house style, Exit row, key line                                                                                                                                                                                                                                                     | teams, sessions, ccy (re-attach offer)                             |
| `internal/registry` | `ccy_registry_dir`, `ccy_registry_read`, `ccy_registry_write`, `ccy_registry_set_restore`, `ccy_registry_remove` (SR L38-594); later `replay_args`, `restore_args`, the restore manifest, `ccy_registry_restore`                                                                                                                                              | teams (read, set_restore), sessions (all), ccy (write, trampoline) |
| `internal/sudo`     | `sudo_ready`, `as_root`                                                                                                                                                                                                                                                                                                                                       | teams; ccy (relabel fix, seats)                                    |
| `internal/gum`      | `need_gum` env, `gum_ask`, `ask_valid`, `ask_yes`, `explain`, `summary_confirm`                                                                                                                                                                                                                                                                               | teams; ccy's seat chooser (Task 4.8)                               |
| `internal/gitx`     | `project_here`, `--show-toplevel`, `the_remote_repo`                                                                                                                                                                                                                                                                                                          | teams, sessions (Ctrl-N), ccy                                      |
| `internal/agentbus` | parsers for `SEAT`, `MEMBER`, `HUMAN`, `DEVICE`, `BACKUP` lines; `seat list/remove/choose/check/take` callers                                                                                                                                                                                                                                                 | teams, ccy (`lib/agent-bus-seats.bash`)                            |
| `internal/declare`  | `declare_helper` wrapper + marker-line parsers                                                                                                                                                                                                                                                                                                                | teams                                                              |
| `internal/systemd`  | `list-units`, `is-active`, `systemd-run --user --scope` launch                                                                                                                                                                                                                                                                                                | teams, sessions (reboot, restore units)                            |

Keep the Exit-code and stderr conventions in one place (`internal/term` or `internal/cli`).

## 8. Known bugs and oddities: fix, do not port

01. **Function-order dependence** (fd159607): subcommands are dispatched at L1203 before
    `homeserver_state`, `collect`, `in_picker`, etc. are defined; start-up offers needed `back_to_list`
    moved up. Go has no such hazard; do not reproduce the split dispatch.
02. **Ctrl-N header** (L1854-1858): says `in <here>` with here = $PWD (a subdirectory is named),
    but `new_session` starts ccy at the checkout's top (L1677). Say the top. The header is built
    once, so it can never change mid-session either (fine), and Ctrl-N stays bound when the line says
    "unavailable".
03. **Ctrl-E failure exits 1 implicitly**: `launch_element` failure at L1800 kills the script through
    `set -e`; TC asserts rc=1 [772]. Make it explicit (keep the exit code).
04. **The MAX_TRIES counter** (L1869, L1933-1937) counts any Ctrl-N/Ctrl-E/Enter-on-seat that did not
    exit, never resets, and ends the picker with "Nothing attached or started after 3 tries." after
    three unrelated no-ops (e.g. Ctrl-N three times outside a project). Decide whether to keep it.
05. **Enter on a ccy member with no known checkout** (kind ccy, dir `-`) goes to attach_seat and says
    `Seat <handle>@<team> has no running session to attach. Ctrl-N starts one.` (L1648), wording for a
    seat, not a member.
06. **cc sessions and host seats**: records with prefix `cc` are skipped (L1263) and only `ccy-*` tmux
    sessions add checkouts (L1253), so `cc --teams` (Task 4.10) sessions never show as holding their
    seat; every seat row's KIND is hard-coded `ccy` (L1436) even for a `.host` handle.
07. **Nondeterministic order** from bash associative arrays: `apply --all`'s "Left alone" and broken
    lists (L923-935), `offer_update`'s "Not as declared" list (L1543), the `--installed` flags. Sort.
08. **Exit codes leak** from declare.py (64, 78) and from agent-bus-install through `set -e`, though the
    usage says 64/130/1 (1.3). TC asserts the 78 [1029]. Either document them or map them; if mapped,
    change TC deliberately.
09. **`print_error`** always emits colour codes (even piped, even with NO_COLOR) and uses `echo -e`, so
    a backslash in a message (a path, a sudo reply) is interpreted. Colour only with `colour_on 2`;
    print literally.
10. **`--list` blank line** after the heading only when not in colour (L1469-1471).
11. **sudo then plain sudo**: backup/restore/remove call `sudo_ready` and then `sudo` without `-n`
    (L986, L1012, L1040, L1082, L1107): an expired timestamp would prompt with no explanation.
    Use `sudo -n` after sudo_ready.
12. **`in_picker` shows internal names**: `(cmd_apply failed, exit 1: see above.)` (L1525). Use the
    user-facing verb (`ccy-teams apply --all failed ...`).
13. **Messages that flash away**: paths that print then `continue` without back_to_list (Ctrl-R,
    Ctrl-X on a human, Ctrl-P on a non-human, "Enter ... does nothing", "That row has changed") are
    redrawn over by fzf at once.
14. **`--org` given as a flag is never looked up on GitHub** (check_owner runs only for the question),
    while `--repo --yes` is checked (L706-714). Check both.
15. **Next steps after after_create**: `next-steps` says "Later, in ccy-teams: Sign in ... Element ..."
    straight after create has just offered both (L749-750).
16. **`split_commas` deletes inner spaces** (`${part// /}`, L483), so `a b` becomes `ab` rather than
    being refused.
17. **New seat number** (L1687-1690) counts numbered podman seats of the team in *any* repository
    (the regex does not check the repo), and duplicates logic that `agent-bus seat choose`
    (checkout.py L662-683) now owns; consider calling `seat choose` instead (its `CHOICE`/`DEFAULT`
    lines), which would also stop the two from drifting.
18. **Two separate `ccy_tmux_list` + registry reads per refresh**, and `collect_teams` is run twice at
    start-up (L1511 and inside `collect`); harmless, but a Go version can read once per loop pass.
19. **restore's terminal test** uses stdin+stdout (L1087, L1097) while remove/create/sudo use
    stdin+stderr: inconsistent; pick one rule.
20. **Row identity by text** (`row_index`, L1633-1642): two identical rows map to the first; and a
    refresh between draw and pick yields "That row has changed". In Go, carry an index column through
    fzf (`--with-nth`) instead, keeping the visible text identical.
21. **The `q:abort` bind** means `q` cannot be typed into fzf's filter (shared picker, TS L836); keep
    for parity, but note it.
22. **`gum choose` empty output** is treated as cancel (L643) though gum exit 0; fine, keep.
