# nord: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its seventh
tool: `nord`, the NordVPN OpenVPN connection manager over NetworkManager. **Method: the legacy
bash is not an oracle of wording.** This document states the intended behaviour as numbered,
testable expectations (`E*`). The bash (`files/home/.local/bin/nord`, 505 code lines, 18
functions, at `36889021f`) was read to find the requirements; where it was wrong, the
expectation states the right behaviour and section 5 lists the departure (`B*`). The proof is
the Go tests in `src-go/internal/nord` and `src-go/cmd/nord`. What only a host can prove is
section 7.

## 1. Why nord now

Left in P6 after `reclaim`: `nord` (505 lines, 18 functions, churn 8), `nvidia-status` (430
lines, 4 functions) and the `ssh-with-key` family. `nord` is the largest and the only
interactive one. Its danger is what it does to the machine's networking: which `nmcli`
commands run, in what order, after which answer, and where the password goes. A recorded
command list proves all of that without a VPN; the connection itself is the owner's (H4-H7).

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/nord`, built by
  `tasks/build-go-command.yml` from `play-nordvpn-openvpn.yml` (which now imports
  `play-golang.yml` first), installed at `~/.local/bin/nord`, mode 0755. The bash file is
  deleted and its row leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv`
  gets `nord	qa-go` (the bash had no suite).
- **D2. NetworkManager is the one source of truth.** Which connection is up is asked of
  `nmcli` every time; the bash's state file `~/.config/nordvpn/.current-connection` is neither
  read nor written, and the play removes it (B5).
- **D3. One seam for processes, owned programs by their owners.** Everything runs through
  `internal/execx`; `systemctl` through `internal/systemd` (`UnitActive`). `nord` joins the
  owners of `nmcli` in the project rule's table (`project_seam_commands`): managing its own
  VPN profiles is a job of its own, beside lxcfreeze's (inside a container) and ftpcamera's
  (the hotspot). `curl` (the public address, E19) is unowned.
- **D4. The password is stored in the profile, and never in an argv** (the lead's decision:
  a person connecting from GNOME's network settings must not start being asked for it). It
  is read from the credentials file at import and given to nmcli's editor
  (`nmcli connection edit`) on its standard input, an `execx.Feed` (E13, B3).
- **D5. The chooser is a numbered menu on `/dev/tty`** through `ui.Lines.Choose` (the shared
  numbered menu: every choice a visible line with its key, a bad key asked again, three tries;
  InteractiveScripts.md rules 1, 2 and 14), each screen with a `ui.Heading` (rule 15).

## 3. Expectations

### Command line

- E1. `nord [--debug] [<command> [<name>]]`. The commands: `list`, `list-active`,
  `connect <name>`, `switch <name>`, `disconnect`, `status`, `cleanup`; no command opens the
  chooser (E22). `-h`/`--help` prints the usage on stdout and exits 0; `-v`/`--version` prints
  `nord 2.0.0` on stdout and exits 0; both are recognised only before the command, as
  `--debug` is.
- E2. An unknown option or command, a missing `<name>` for `connect` or `switch`, or an
  argument more than the command takes, is `nord: <what> — see 'nord --help'.` on stderr,
  exit 64. A missing name is followed on stderr by the config names (E5), so the person can
  pick one. Every error line is `nord: <text>` on stderr.
- E3. `--debug` prints `nord: run: <argv>` on stderr before each command nord runs (never the
  password: it is in no argv, D4).
- E4. Exit codes: 0 done, 1 failed (or `status` not connected, E19), 64 usage, and on a
  signal (Ctrl-C and the others `execx.SignalContext` catches) the signal's code (130 for
  SIGINT), after `Cancelled.` on stderr (E24). A failed write to stdout or stderr fails a run
  that would succeed (exit 1).

### Configs and their names

- E5. The configs are the regular files directly in `~/.config/nordvpn/configs/` whose names
  end `.ovpn` (a symlink to a regular file counts), in byte order. A config's name is its file
  name less `.ovpn` and less a trailing `.nordvpn.com.udp` or `.nordvpn.com.tcp`
  (`uk1234.nordvpn.com.udp.ovpn` is `uk1234`). When two configs would share a name
  (`uk1234.nordvpn.com.udp.ovpn` and `uk1234.nordvpn.com.tcp.ovpn`), each is named by its
  file name less `.ovpn` instead (`uk1234.nordvpn.com.udp`, `uk1234.nordvpn.com.tcp`) (B1).
- E6. A `<name>` given to `connect` or `switch` (or chosen, E22) is a config's name, or the
  file name less `.ovpn` of a config (the long form the docs show), which means the same
  config. A short name two configs share is `nord: '<x>' names more than one config: <a>, <b>; use one of those names.`, exit 1. A name no config has is
  `nord: no config named '<x>' in ~/.config/nordvpn/configs/.` and the names on stderr
  (`Available configs:` and one per line, or the E7 hint when there are none), exit 1. A name
  is only ever matched against the listing, so a `/` or `..` in it reaches no file (B2).
- E7. **`nord list`** prints on stdout `Available VPN configs:`, a blank line, `  <name>` for
  each config, a blank line and `Total: <n> configs`, then either the hint
  (`Download .ovpn files from NordVPN and place them in:` / `  ~/.config/nordvpn/configs/` /
  `Visit: nordvpn.com → Dashboard → Downloads → OpenVPN configs`) when there are none, or
  `To connect: nord connect <name>` and `Or run 'nord' for the interactive chooser.`. A
  configs directory that is missing or cannot be read is an error naming it and the play
  (E9), exit 1.

### What each command needs

- E8. Each command checks what it uses before it acts, and reports every missing piece, not
  only the first, then exits 1: `list` the configs directory; `list-active`, `status`,
  `disconnect` and `cleanup` `nmcli` on `PATH` and NetworkManager running
  (`systemctl is-active --quiet NetworkManager` through `internal/systemd`); `connect`,
  `switch` and the chooser all of those, `openvpn` on `PATH`, the configs directory, and the
  credentials (E10) (B6).
- E9. Each failed check is one line saying what is missing and what to do:
  `nord: nmcli is not installed; nord manages its connections through NetworkManager.`,
  `nord: NetworkManager is not running; nord manages its connections through it.`, a
  `systemctl` that could not run is `nord: could not ask systemd whether NetworkManager is running: <reason>`; `openvpn` missing and the configs directory missing each name
  `./playbooks/imports/optional/common/play-nordvpn-openvpn.yml`, which installs and creates
  them. Never a manual install command (B7).
- E10. The credentials file is `~/.config/nordvpn/.credentials`: the service username on line
  1 and the password on line 2, each ending at a newline or the file's end (a `\r` before the
  newline is dropped; nothing else is trimmed); blank lines after line 2 are ignored. A file
  that is missing or unreadable, or whose line 1 or 2 is empty, or that has a third non-blank
  line, is `nord: <the file>: <what is wrong>` and a line naming the play that writes it, exit
  1, before anything is imported or brought down (B4). Neither value is ever printed.

### NetworkManager

- E11. nord's profiles are the NetworkManager connections of type `vpn` whose name starts
  `nordvpn-`, read from `nmcli -t -f NAME,UUID,TYPE connection show`; the active ones from
  `nmcli -t -f NAME,UUID,TYPE,DEVICE,STATE connection show --active`. The terse fields are
  split at unescaped `:` and unescaped (`\:` is `:`, `\\` is `\`). An `nmcli` listing that
  fails is `nord: nmcli could not list the connections: <reason>`, exit 1. Every command
  after the listing names a profile by its UUID, never by its name (B8).
- E12. Two profiles with the name `nordvpn-<name>` are
  `nord: two NetworkManager connections are named nordvpn-<name> (<uuid>, <uuid>); run 'nord cleanup' and connect again.`, exit 1, and nothing is changed.

### connect and switch

- E13. **`nord connect <name>`** resolves the config (E6) and checks the credentials (E10).
  When there is no profile `nordvpn-<name>`, it is imported: `Importing <name>...` on stderr,
  `nmcli connection import type openvpn file <path>`, whose output names the new profile's
  UUID (`Connection '<id>' (<uuid>) successfully added.`); an import that fails, or whose
  output names no UUID, fails with nmcli's output, exit 1. Then
  `nmcli connection modify uuid <uuid> connection.id nordvpn-<name> +vpn.data username=<user>`, then the password is stored in the profile through
  nmcli's editor, `nmcli connection edit uuid <uuid>` with the lines
  `set vpn.secrets password=<password>`, `save persistent` and `quit` on its standard input
  (D4); the session succeeds only when nmcli exits 0 and prints `successfully updated`. When
  either step fails, the imported profile is deleted (`nmcli connection delete uuid <uuid>`),
  so no half-configured profile is left (its failure named too), and the run fails, exit 1
  (B9); nmcli's words are shown with the password replaced by `<password>`. `execx` refuses to
  start the editor when the password is found inside its argv (a password `edit` would be), so
  a password that could be read there never is. `Imported nordvpn-<name>.` follows a success.
- E14. Then every other active nord profile is brought down
  (`Disconnecting <other>...`, `nmcli connection down uuid <uuid>`); one that fails stops the
  run before the new connection is brought up (`nord: could not disconnect <other>: <reason>; not connecting <name> beside it.`, exit 1) (B10). Then
  `Connecting to <name>...` and `nmcli connection up uuid <uuid>`, the password the profile's
  own (E13). Success is
  `Connected to <name>.`, exit 0. Failure is `nord: could not connect to <name>: <nmcli's output>` and the troubleshooting lines
  (`journalctl -u NetworkManager --no-pager --since '5 minutes ago'`, then
  `nord cleanup && nord connect <name>` to import it again), exit 1 (B11).
- E15. A profile already active and `activated` is `Already connected to <name>.` and nothing
  runs that changes anything, exit 0 (the bash brought it up again).
- E16. **`nord switch <name>`** is `connect` (E13-E15): connect already brings every other
  nord connection down first, after the new config is found and imported, so a bad name or a
  failed import leaves the current connection up (B12).

### disconnect, status, list-active, cleanup

- E17. **`nord disconnect`** brings down every active nord profile (`Disconnecting <name>...`,
  `nmcli connection down uuid <uuid>`, then `Disconnected <name>.`). None active is
  `No NordVPN connection is active.`, exit 0. Any that fails is
  `nord: could not disconnect <name>: <reason>`, the others are still tried, exit 1 (B13).
- E18. **`nord list-active`** prints on stdout `Imported NetworkManager connections:`, a blank
  line, then `  <name>` for each nord profile (the name less `nordvpn-`), ` (connected)` after
  an activated one; none is `  (none)`, a blank line and
  `Use 'nord connect <name>' to import and connect.`. Exit 0.
- E19. **`nord status`** prints on stdout, for each active nord profile,
  `Connected: <name>` (or `Connecting: <name> (<state>)` when not yet `activated`) and
  `Device: <device>` when nmcli names one; then, when one is activated,
  `Public IP: <address>` from `curl -fsS --max-time 5 https://api.ipify.org`, its reply
  accepted only when it is an IP address; otherwise `Public IP: unknown (<why>)` (curl missing,
  failed, or answered something else). Exit 0. None active prints `Not connected` and exits 1.
- E20. **`nord cleanup`** prints `Removing all nordvpn-* connections from NetworkManager...`
  on stderr, then for each nord profile `  Removing nordvpn-<name>` (` (connected: this disconnects it)` when active) and `nmcli connection delete uuid <uuid>`; every profile is
  tried. None is `No nordvpn connections found.`, exit 0. All removed is
  `Removed <n> nordvpn connection(s).`, exit 0; any that failed is named with nmcli's reason,
  then `nord: removed <k> connection(s); <f> could not be removed.`, exit 1.

### The log

- E21. The commands that change something (`connect`, `switch`, `disconnect`, `cleanup`, and
  the chooser) open `~/.local/share/nordvpn/nord.log` for appending (mode 0600, its directory
  made when missing) before they act; one that cannot be opened is
  `nord: cannot write the log <path>: <reason>`, exit 1, and nothing is run. A log over
  1 MiB is first renamed to `nord.log.old` (replacing it). Every line those commands print on
  stderr (progress, errors, `--debug`) is appended as `<time, RFC 3339> <line>`. A write to the
  log that fails later is said once and the run exits 1 when it ends. The credentials are
  never logged (they are never printed).

### The chooser

- E22. **`nord`** with no command needs a terminal: stdin a terminal and `/dev/tty` opened;
  otherwise `nord: the chooser needs an interactive terminal; see 'nord --help' for the commands.`, exit 1. It checks what `connect` needs (E8), lists the configs (none:
  `nord: no VPN configs in ~/.config/nordvpn/configs/.` and E7's hint, exit 1) and the active
  nord profiles (E11).
  - Not connected: the heading `nord › Connect` with the context `<n> servers`, then the menu
    `  1) <name>` ... and `  q) Cancel`, asking `Choose a server [1-<n>, q]` (`[1, q]` for
    one).
  - Connected: the heading `nord` with the context `connected to <names>`, then
    `  1) Switch to another server`, `  2) Disconnect`, `  q) Cancel`, asking
    `Choose [1, 2, q]`. `1` is the server menu with the heading `nord › Switch`, each active
    one marked ` (connected)`; `2` is `disconnect` (E17).
  - A server chosen is `connect <name>` (E13-E15).
  - `q` is `Nothing changed.`, exit 0. A key not in the menu is asked again (`ui.Lines`:
    `Choose one of: ...`), three tries; after the third,
    `nord: no valid choice after 3 tries; nothing changed.`, exit 1. The end of input is
    `The input ended before a choice; nothing changed.`, exit 130 (`ui.ErrInputEnded` is a
    cancel) (B14).

### Output and interrupts

- E23. stdout carries only `list`, `list-active` and `status` reports, the usage and the
  version; progress, menus, questions and diagnostics go to stderr.
- E24. The command runs under `execx.SignalContext`. A command that changes the machine
  (`nmcli connection import`, `modify`, `up`, `down`, `delete`) is not signalled or killed by
  nord: the run waits for it to end and then runs nothing after it, except the delete that
  undoes a half-made import (E13), which still runs. A command that only reads is stopped,
  and a wait at the menu ends at once. The run then prints a blank line and `Cancelled.` and
  exits with the signal's code.

## 4. Kept as it was

The commands and their names, the `nordvpn-` prefix and the profile names (`nordvpn-<name>`
for the short name), the paths of the configs, the credentials and the log, the log's 1 MiB
rotation to `nord.log.old`, the import on first connect, the username and the password stored in the profile (the password now through stdin, B3),
`status` exiting 1 when not connected, the public address from api.ipify.org, `--debug`, and
the help's examples.

## 5. Departures from the bash

- B1. Two configs that strip to the same name (the UDP and TCP file of one server) each keep
  their long name, and the short one is refused as ambiguous (E5, E6). The bash listed the
  short name twice and silently connected the UDP one whichever line was meant.
- B2. A name is matched against the configs listing, never joined into a path (E6). The bash
  built `$OVPN_DIR/${name}.ovpn`, so `nord connect ../x` read outside the directory.
- B3. The password never reaches an argv (D4). The bash ran
  `nmcli connection modify ... +vpn.secrets password=<password>`, visible to every user in
  `/proc/<pid>/cmdline` while it ran, and stored it in the profile. The port stores it in the
  profile too, as the bash did, so connecting from GNOME's network settings asks for nothing,
  but through nmcli's editor fed on stdin (E13). The bash also swallowed that step's failure
  (`|| true`); a failed or unsaved store now deletes the import (B9). As before, new
  credentials from the play reach a profile only when it is imported again (`nord cleanup`).
- B4. The credentials are read and checked before anything is imported or brought down
  (E10). The bash imported first and read them after, used `head -1` and `tail -1` (a
  one-line file gave the username as the password), and never noticed a third line.
- B5. No state file (D2). The bash's `.current-connection` named the connection it last
  brought up; once it was stale (a connection dropped, or brought up from GNOME), `connect`
  brought down the wrong one or none, and `disconnect` ignored it anyway.
- B6. Each command checks only what it uses (E8): `nord list` works without NetworkManager.
  The bash checked everything for every command (and `openvpn` for `nord status`).
- B7. A missing dependency names the play, not `sudo dnf install ...` or
  `sudo systemctl start ...` (the project's IaC rule).
- B8. Profiles are named by UUID after the listing (E11, E12). The bash found the imported
  profile by `grep -F <file base> | head -1` over every connection's name, which could pick an
  older profile, and acted on names, which NetworkManager does not keep unique.
- B9. A failed configuration after the import deletes the imported profile (E13). The bash
  left it, unconfigured, named after the file, and the next `connect` imported a second one.
- B10. A connection that cannot be brought down stops the connect (E14). The bash logged it at
  debug level and brought the new one up beside it: two VPNs, or one that failed over the
  other.
- B11. nmcli's output is shown when it fails (E13, E14). The bash sent the import's and the
  connect's stderr to `/dev/null`, so a failure said only that it failed. Its troubleshooting
  told the person to `cat` the credentials file, printing the password; and
  `journalctl -f`, which never returns. Both are replaced (E14).
- B12. `switch` finds and imports the new config before bringing the old connection down
  (E16). The bash disconnected first, so a mistyped name left the machine with no VPN.
- B13. `disconnect` brings down every active nord profile (E17). With two active, the bash
  passed both names to one `nmcli connection down` as a single argument, which failed.
- B14. The chooser asks again on a key that is not in the menu, three tries (D5; the bash
  cancelled on any other key, and an out-of-range number was a `[` error), marks the connected
  server, and has a visible `q) Cancel` line (the bash: Enter to cancel).
- B15. Usage errors exit 64 (the bash: 1), and an extra argument is an error (the bash
  ignored it).
- B16. A Ctrl-C waits for the `nmcli` command that changes something, then says `Cancelled.`
  (E24). The bash's trap only exited 130, whatever was half done.

## 6. Proof

Expectation tests: `go test -race` over `internal/nord` and `cmd/nord`; every expectation
has a test written first and seen to fail (journal), over `execx.Fake` (every `nmcli`,
`systemctl` and `curl` argv asserted, with the password's `Feed`), a scripted terminal and a
temporary home holding the configs, the credentials and the log. A command the test does not
script fails it (`execx.ErrUnexpected`), so a changing command nobody expected is caught.
There is no oracle against the bash: the two share no on-disk format nord still uses but the
configs directory and the credentials file, both read-only and covered by E5 and E10, and the
profiles' names, covered by E11 and E13.

## 7. Host checks

Proven only on the host, after `play-nordvpn-openvpn.yml` has run. A check that changes the
machine's network is the owner's: bringing a VPN up or down reroutes every connection the
host has, the host agent's own included, so it is neither harmless nor the agent's to undo.

- H1 (host agent). `~/.local/bin/nord` is an ELF; `nord --version` prints `nord 2.0.0`;
  `nord --help` prints the usage; `nord bogus` exits 64; `~/.config/nordvpn/.current-connection`
  is gone.
- H2 (host agent, read-only). `nord list` lists the configs on disk (or the hint, exit 0);
  `nord list-active` lists the imported profiles; `nord status` prints the state and exits 0
  connected or 1 not, matching `nmcli -t -f NAME,TYPE,STATE connection show --active`.
- H3 (host agent, tmux). `nord` in a tmux pane, driven by `send-keys` and read with
  `capture-pane`: the menu shows (connected or not, as H2 found); `x` three times exits 1;
  `q` exits 0; Ctrl-D exits 130; Ctrl-C at the menu prints `Cancelled.` and exits 130. No
  `nmcli` command that changes anything runs (`nmcli connection show` the same before and
  after).
- H4 (owner). `nord connect <a server not yet imported>`: it imports, connects, and
  `nord status` shows the NordVPN address; `ps` during the connect shows no password in any
  argv; the profile holds the username and the password
  (`nmcli -s -g vpn.secrets connection show nordvpn-<name>` names it), so the editor fed on
  stdin was accepted (E13); and connecting it from GNOME's network settings asks for no
  password.
- H5 (owner). `nord switch <another>` brings the first down and the second up;
  `nord switch nosuch` fails and leaves the connection up (B12).
- H6 (owner). `nord disconnect` brings it down; `nord status` exits 1.
- H7 (owner). `nord cleanup` removes every `nordvpn-*` profile, and a following
  `nord connect` imports again (and a profile the bash imported, with its stored password,
  connects before the cleanup, B3).
