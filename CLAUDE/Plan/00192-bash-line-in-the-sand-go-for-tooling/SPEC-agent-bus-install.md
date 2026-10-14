# agent-bus-install: functional specification for the Go port

Source of truth: `files/usr/local/sbin/agent-bus-install` (1180 lines, 15 commits, last change
`1cb500f5`). `L<n>` means a line of that file unless another file is named. Parity oracle:
`scripts/test-agent-bus-install.bash` (AI; 877 lines, 206 `check` calls, 274 runtime
assertions once the loops run). Method and depth follow `SPEC-ccy-teams.md` of Plan 00191.

Three premises in the brief of this step do not hold, and the port is shaped by the real
facts:

1. **The sudoers rule does not cover the installer.** `files/etc/sudoers.d/agent-bus-read-only.j2`
   grants the desktop user exactly two read-only `agent-bus` (the admin wrapper) argv shapes,
   `list <team>` and `human devices <team> <name>`. `agent-bus-install` is not in it. ccy-teams
   reaches the installer with `sudo -v` (password, once) and then `sudo -n /usr/local/sbin/agent-bus-install ...` (`src-go/internal/sudo`, `src-go/internal/agentbus`),
   i.e. under the user's ordinary full sudo. The comment at `src-go/internal/sudo/sudo.go:18`
   ("the two root-owned programs the sudoers rule covers") is wrong about `InstallTool`
   (bug B1). The rule keeps working through the port because the port does not touch it.
2. **The installer calls neither `declare.py` nor `element.py`.** Those belong to ccy-teams
   (`src-go/internal/declare` execs `declare.py`; `element.py` is the Element profile code
   behind `play-agent-bus-element.yml`). The installer calls exactly three Python things:
   the zipapp builder `helpers/pingbus/bundle.py`, the admin zipapp `agent-bus.pyz`
   (`render check|toml|dropin|stamp`, `bootstrap`) through the root wrapper, and two inline
   `python3 -I -c` snippets. The only coupling to `declare.py` is a digest (section 8.3).
3. **It "sources" nothing.** It is one self-contained script with no `source` line.

Placeholders: `<team>` is a team name, `<checkout>` the fedora-desktop checkout, `<prefix>` the
test prefix. The suite's fixture team `alpha` and its RFC 5737/3849 documentation addresses are
the repository's own test data.

---

## 0. Deployment and callers

| Fact                     | Where                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deployed copy            | `/usr/local/sbin/agent-bus-install`, 0755 root:root, installed **by the installer itself** (`software`, L494) as a byte copy from `<checkout>/files/usr/local/sbin/`.                                                                                                                                                                                             |
| Run in place by the play | `playbooks/imports/play-agent-bus.yml` L45 `agent_bus_installer: "{{ root_dir }}/files/usr/local/sbin/agent-bus-install"`, play-level `become: true`, so root executes a user-writable checkout file.                                                                                                                                                             |
| Play calls               | `software --source <checkout> [--bus-address IP]` (L88-95); `remove --team T [--purge]` per absent entry (L119-129); `team --team-file <tmpfile>` per present entry (L157-168). `changed_when` = any stdout line matching `^CHANGED\t`.                                                                                                                           |
| Play also installs       | the sudoers rule (L100-108, `visudo -cf`) and the LXC members (`helpers.agent_bus_lxc.members`, which only quotes the installer in an error text).                                                                                                                                                                                                                |
| systemd                  | `files/etc/systemd/system/agent-bus-backup@.service` ExecStart `/usr/local/sbin/agent-bus-install backup-now --team %i` (oneshot, `Requisite=agent-bus-hs@%i.service`); `agent-bus-backup@.timer` (daily, `RandomizedDelaySec=1h`, `Persistent=true`). `agent-bus-hs@.service` names the installer only in a comment. The unit files are not changed by the port. |
| ccy-teams (Go)           | `Client.Backups`, `BackupNow`, `Restore`, `RemoveTeam` (`src-go/internal/agentbus/agentbus.go:171-202`), each `sudo -n /usr/local/sbin/agent-bus-install <sub> --team <team> [...]` via `sudo.Run`/`AsRoot`. `apply` and `create` run the plays, not the installer.                                                                                               |
| Documents that name argv | `docs/agent-bus.md` L197-213, L466-484 (`software --source <clone>`, `team --team-file`, `check`, `remove`, `backup-now`, `restore`, `backups`); the member kit READMEs. `tests/helpers/agent_bus/test_member_docs.py` parses every such invocation against the installer's `--help` (section 10.3).                                                              |
| Plan 00161 scripts       | `CLAUDE/Plan/00161-agent-team-bus-matrix/{deploy.bash,acceptance.bash,_deploy-steps.inc.bash}` run `<checkout>/files/usr/local/sbin/agent-bus-install` directly (cut-over, section 13).                                                                                                                                                                           |
| Drift gate               | `scripts/qa-deployed-drift.bash` L233 pairs the repo file with `/usr/local/sbin/agent-bus-install` (cut-over, section 13).                                                                                                                                                                                                                                        |

Documented use outside this repository: "other projects run it from a clone at a pinned commit"
(header L7-8, `docs/agent-bus.md` L197). A Go binary changes that to "build, then run" (decision D5).

## 1. Commands, flags, usage, exit codes

### 1.1 Usage text

`usage` (L105-134) prints the heredoc verbatim, starting `Usage: agent-bus-install COMMAND [OPTIONS]` and ending `Exit: 0 done, 1 failed, 64 usage, 77 not root, 78 configuration.`. The Go
binary embeds it as a `usage.txt` (copy L106-133 byte for byte). It goes to **stdout, exit 0**
for `-h`, `--help`, `help`; to **stderr, exit 64** for no arguments. Its command lines have the
shape `  <command> <options...>` (two spaces, command, then the options in `--name VALUE` /
`[--name VALUE]` form); `test_member_docs.py::installer_synopsis` parses exactly that shape.
Keep the shape or change that test in the same commit.

Subcommand table (option kinds from the `parse_options` specs):

| Command      | Options (`value` takes an argument, `flag` none) | Required      | Root | Fedora check |
| ------------ | ------------------------------------------------ | ------------- | ---- | ------------ |
| `software`   | `--source` value, `--bus-address` value          | `--source`    | yes  | yes          |
| `team`       | `--team-file` value                              | `--team-file` | yes  | yes          |
| `remove`     | `--team` value, `--purge` flag                   | `--team`      | yes  | no           |
| `backup-now` | `--team` value                                   | `--team`      | yes  | no           |
| `restore`    | `--team` value, `--backup` value                 | both          | yes  | no           |
| `backups`    | `--team` value                                   | `--team`      | yes  | no           |
| `check`      | `--team` value                                   | `--team`      | yes  | no           |

### 1.2 Dispatch and option parsing (L1160-1178, L141-177)

- No argument: usage to stderr, exit 64. First argument `-h`/`--help`/`help`: usage to stdout,
  exit 0 (any further arguments ignored). Any other first argument:
  `agent-bus-install: unknown command '<x>' (see --help)`, exit 64 (an empty first argument is
  `unknown command ''`).
- `parse_options` (all errors exit 64, message prefix `agent-bus-install: `):
  - an argument not starting `--`: `unexpected argument '<arg>' (see --help)` (so `-h` after a
    subcommand, and any positional, is this error);
  - `--name=value` and `--name value` both work; the name is split at the first `=`;
  - unknown name: `unknown option --<name> (see --help)` (so `software --help` is this error);
  - repeated: `--<name> given twice`;
  - flag with `=value`: `--<name> takes no value`;
  - value option with no following argument: `--<name> needs a value`;
  - empty value (`--name=` or `--name ""`): `--<name> needs a non-empty value`;
  - a value may itself start with `--` (`--source --frob` takes `--frob` as the value). Keep.
- `require_option NAME CMD`: `<CMD> needs --<NAME>` (exit 64). The CMD word is `software`,
  `team` (for `--team-file`), `remove`, `backup-now`, `restore`, `backups`, `check`.
- `team_option CMD`: requires `--team`, then `--team '<x>' is not a team name (^[a-z][a-z0-9-]{0,23}$)` (64). The pattern must stay anchored with end-of-text `$` (Go
  `regexp` without `(?m)` does this; Python's `$` would admit a trailing newline).
- Order inside each command matters because the suite pins exit codes: parse, required options,
  option validity (team name, backup id, bus address), **then** root, **then** Fedora, **then**
  everything else. A non-root caller therefore gets usage errors (64) before the not-root
  refusal (77).

### 1.3 Exit codes (L28-32 and the leaks)

| Code  | Meaning                                                                                                                                                                                                                                                                                                                                       |
| ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0     | done (including "nothing to do" and `backups` with no backup)                                                                                                                                                                                                                                                                                 |
| 1     | a step failed (`EXIT_FAIL`); also `check` with a FAIL fact (after printing all facts)                                                                                                                                                                                                                                                         |
| 64    | usage                                                                                                                                                                                                                                                                                                                                         |
| 77    | not root (`run it as root: sudo agent-bus-install ...`)                                                                                                                                                                                                                                                                                       |
| 78    | configuration: not Fedora, a refused team file, a missing or malformed pin, a source that is not a checkout, a port conflict, a missing team directory or backup, a refused state tar                                                                                                                                                         |
| other | **leaked** by `set -e` from an unguarded child: curl (22, 6, 7, 28 ... and 77/78, which collide with the two codes above), `jq` (2-5), `nmcli`, `dnf`, `useradd`, `install`. Only the admin tool's status is meant to pass through: `render check` (`die "$status"`, L776) and `bootstrap` (L831, L1094) propagate 64/69/70/75/77/78. Bug B3. |

ccy-teams classifies by code (`agentbus.kindOf`: 64 usage, 69 unreachable, 70 refused, 75 busy,
77 not-root, 78 config, anything else failed). The play and systemd only test non-zero.

## 2. Privilege model and the checks that guard root

### 2.1 Who runs what

- The installer needs **EUID 0** (`require_root`, L209-213) except under the test prefix (3.2).
  It is not setuid, has no sudoers rule of its own and drops no privilege itself.
- Reached as root by: Ansible `become` (software/remove/team), systemd (backup-now), and the
  user's password sudo from ccy-teams (backups, backup-now, restore, remove). There is no
  NOPASSWD path to it. **Port rule: do not add one.** The sudoers file stays as is.
- What the installer delegates to a lesser identity: every account operation goes through
  `/usr/local/bin/agent-bus` (the root wrapper, `files/usr/local/bin/agent-bus`), which does
  `runuser -u agent-bus -- /usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz ...`. The
  homeserver runs as `agent-bus` under the hardened unit (`agent-bus-hs@.service`: `User=agent-bus`,
  `NoNewPrivileges`, `ProtectSystem=strict`, `IPAddressDeny=any`, `SocketBindDeny=any`, plus the
  per-team drop-in that allows only the listen addresses). The restore one-off unit gets the
  same sandbox (L941-954, L1084-1085).
- Trust boundaries the code defends, in order of importance:
  1. **The `agent-bus` user (and so a compromised homeserver) owns `/var/lib/agent-bus/<team>`.**
     Root writes into it (`tuwunel.toml`, `team.json`, the secret, the restore) and reads from
     it (backup tars, `team.json` of other teams, `meta/` numbers). Hence: team state tars are
     kept in root's own `/var/lib/agent-bus-install/<team>/backups/`; the firewall record and the
     binary/unit records live in `/var/lib/agent-bus-install/<team>/`, "kept out of the agent-bus
     user's directory so the homeserver cannot steer what root later removes" (L86-87);
     `ensure_dir`/`put_file` refuse a symlink where a directory/file is expected (L233, L258).
  2. **The state tar is validated before anything is stopped** (`check_state_tar`, L911-937).
  3. **Names that become paths or unit names are regex-validated** (2.2).
  4. **The pinned Tuwunel hash** is the supply-chain check: the release is unsigned, so the
     sha256 of the downloaded `.zst` must equal the pin (L376).
  5. **Root runs code from the user-writable checkout** (the installer file in the play, and
     `python3 -m helpers.pingbus.bundle` with `cwd=<source>`, L388). This is the existing trust
     boundary of every play in the repository, not something the installer can fix. The port
     narrows it (section 13: the play runs a root-owned copy of the binary).

### 2.2 Argument validation that guards root (port must reproduce each exactly)

| Input                                 | Rule                                                                                                                                                                                                                  | What it protects                                                                                                                                                                                                                                                             |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--team` (all commands except `team`) | `^[a-z][a-z0-9-]{0,23}$`, exit 64 (L45, L183-186)                                                                                                                                                                     | `rm -rf /var/lib/agent-bus/<team>` and `/var/lib/agent-bus-install/<team>` (purge), unit names `agent-bus-hs@<team>.service`, `agent-bus-restore-<team>.service`, paths of tars, drop-in directory; no `/`, no `.`, no leading `-`, so no traversal and no option injection. |
| team name in the file (`team`)        | \`jq -er '.team                                                                                                                                                                                                       | strings'`then the same regex, exit 78 (L767-770); the name is then re-validated by`render check\`                                                                                                                                                                            |
| `--backup`                            | `^[1-9][0-9]*$`, exit 64, checked before root (L1039)                                                                                                                                                                 | the path `<team>/backups/meta/<id>`, the tar name `agent-bus-state-<id>.tar`, and the `--restore-backup <id>` argv given to Tuwunel. No leading zero, no sign, no whitespace.                                                                                                |
| `--bus-address`                       | `valid_bus_address` (L190-201): parses as an IP, `str(addr) == text` (canonical form), and not unspecified, loopback, link-local or multicast; exit 64                                                                | the `ipv4.addresses`/`ipv6.addresses` handed to NetworkManager and the future listen address.                                                                                                                                                                                |
| `--source`                            | absolute (`[[ $source == /* ]]`, exit 64, L464) and must hold the 15 needed files, exit 78 (L468-472)                                                                                                                | a relative or foreign tree.                                                                                                                                                                                                                                                  |
| `--team-file`                         | regular and readable, exit 64 (L760); content is validated by the admin tool (`render check`, which enforces listen addresses, CIDRs, ports, server_name stability, repos, humans)                                    | the whole team definition. The installer does **not** re-validate listen addresses or CIDRs itself; it relies on `render check`'s 78. The Go port must keep that division: validation stays in the Python admin tool until that is ported (D2).                              |
| pin file                              | each line `key: value`, keys `tuwunel_version` (`^v[0-9]+\.[0-9]+\.[0-9]+$`), `tuwunel_sha256_x86_64`, `tuwunel_sha256_aarch64` (`^[0-9a-f]{64}$`), unknown key or malformed value or missing key: exit 78 (L310-328) | the URL and file names built from the version.                                                                                                                                                                                                                               |
| state tar                             | see 5.7; validated in root's own directory before the homeserver is stopped                                                                                                                                           | a tar planted by the homeserver that would write outside the state or create setuid files as root.                                                                                                                                                                           |

### 2.3 The sudoers rule, exactly

```
{{ user_login }} ALL=(root) NOPASSWD: /usr/local/bin/agent-bus ^list [a-z][a-z0-9-]*$, \
    /usr/local/bin/agent-bus ^human devices [a-z][a-z0-9-]* [a-z][a-z0-9_-]*$
```

Mode 0440 root:root in `/etc/sudoers.d/agent-bus-read-only`, validated with `visudo -cf`.
Anchored regexes (sudo >= 1.9.10): `agent-bus list <team>` and `agent-bus human devices <team> <name>`; no other argument, subcommand or option reaches the wrapper. **It names the wrapper
only.** The installer has no NOPASSWD grant; ccy-teams' calls to it need the cached credential
from `sudo.Ready`. The Go binary needs no change for the rule to keep working: it never
mentioned the installer and the wrapper's path and mode are untouched.

### 2.4 Residual risks the port should close (fix deliberately, no visible change on honest input)

- **PR1 test hook in production.** `AGENT_BUS_INSTALL_TEST_PREFIX` skips the root check, keeps
  the caller's PATH and takes the admin tool from PATH (L62-77). Under sudo's `env_reset` it is
  stripped, but a root caller that carries it relocates every path and trusts PATH. The Go
  production binary must not read it: compile the hook in only under a build tag (section 12.4).
- **PR2 time-of-check/time-of-use under the agent-bus directory.** `[[ -L $dest ]]` then
  write, in a directory the agent-bus user can rename in. Use `os.Root` (Go >= 1.24; `go.mod` is
  1.26) rooted at `/var/lib/agent-bus/<team>` for every write and read below it, so a symlink
  swapped in cannot redirect root. Removal with `os.RemoveAll` is already no-follow.
- **PR3 work directory.** `mktemp -d` honours an inherited `TMPDIR`; the restore stage holds the
  secrets (L1054-1060). Create the work directory under `/var/lib/agent-bus-install/.work-*`
  (root 0700, same filesystem as the destination, so the final move is a rename) instead.
- **PR4 interpreter environment.** `python3 -B -s -m ...` for the bundler honours `PYTHONPATH`
  and friends (the inline snippets use `-I`). Run the bundler with `PYTHONPATH`, `PYTHONHOME`,
  `PYTHONSTARTUP`, `PYTHONUSERBASE`, `PYTHONPLATLIBDIR` removed (`execx.Cmd.Unset`).
- **PR5 umask.** Not set; modes are explicit everywhere. Set `0o077` at start and keep every
  explicit chmod.

## 3. Environment

### 3.1 Production

`PATH` is reset to `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin` (L71); the admin tool is
`/usr/local/bin/agent-bus` (L72); root-owned paths are owned `root:root`; service-owned paths
`agent-bus:agent-bus`. No other variable is read by the script (`EUID` aside). Every child
inherits the rest of the environment (see PR4). Locale is not pinned; the only
locale-sensitive calls are `sort` (ordering of addresses and ids), which the port does in Go.

### 3.2 Test mode (`AGENT_BUS_INSTALL_TEST_PREFIX`, L62-77)

Set and non-empty: must be an **existing absolute directory** (else `AGENT_BUS_INSTALL_TEST_PREFIX must be an existing absolute directory`, 64). Then: every path below is rooted under it
(`LIB_DIR`, `SHARE_DIR`, `KIT_DIR`, `BIN_DIR`, `SBIN_DIR`, `UNIT_DIR`, `STATE_ROOT`,
`INSTALL_STATE_ROOT`, `APPLIED_ROOT`, `OS_RELEASE`, all via `$PREFIX/...`); owners are the running
user and group (`ROOT_OWNER`, `ROOT_GROUP`, `SVC_OWNER`, `SVC_GROUP`); the root check is skipped;
PATH is kept (stubs win); `ADMIN_TOOL=agent-bus` is looked up on PATH. Not rooted: the
`useradd --home-dir /var/lib/agent-bus`, `/usr/local/lib/agent-bus/tuwunel` in the `systemd-run`
argv (L1085), the `ReadWritePaths` and `Environment=` text read from the unit.

## 4. Output contract

- **stdout** carries only: `CHANGED<TAB><what>` lines (the play's `changed_when`); for `check`
  `CHECK<TAB><fact><TAB>ok|FAIL|info<TAB><value>`; for `backups` `BACKUP<TAB><id><TAB><time>`;
  `--help` text; and **the admin tool's own stdout** from `bootstrap` (its `CHANGED` lines are
  passed through unfiltered, suite L604-606). Suite pins "stdout carries only CHANGED lines" for
  `software` and `team` (L454, L585).
- **stderr**: `agent-bus-install: <message>` for `die`/`say`; every child's chatter (dnf, nmcli,
  firewall-cmd, systemctl enable/disable, journalctl, systemd-run) is redirected `>&2`; the admin
  tool's stderr is inherited (that is the "(above)" in `team file <file> refused (above)`).
- **No secret is ever printed or put in argv.** The shared secret is generated into a 0600 file;
  the suite asserts it never reaches stdout, stderr or the stamp (L586, L593).
- A Go `execx.Runner` captures a child's stdout unless a writer is given: the port forwards only
  the admin tool's `bootstrap`/`render` stdout where bash let it through, and sends every other
  child's streams to stderr, so nothing leaks into stdout by omission.

## 5. Behaviour, command by command

`changed()` writes `CHANGED\t<text>` to stdout at the moment of the change. The texts are
listed because the play, ccy-teams and the suite show or match them (suite pins marked S).

### 5.1 `software --source DIR [--bus-address IP]` (L451-509)

01. Parse; require `--source`.
02. If `python3` is on PATH: validate `--bus-address` now (else after the packages, step 9).
    Refusal: `--bus-address '<x>' is not a concrete, canonical IP address` (64). Suite S: seven
    bad values (`0.0.0.0`, `::`, `192.0.2.300`, `2001:db8::zz`, `127.0.0.1`, `192.0.2.010`, empty)
    all 64 with **no external command run at all**.
03. Root (77); Fedora: `<OS_RELEASE>` readable else `<path> is missing: only Fedora is supported`
    (78); `ID=` (quotes stripped) must be `fedora` else `<path> says ID=<id|<none>>: only Fedora is supported` (78). S: names `os-release`.
04. `--source` absolute (64). Needed files, each `[[ -f ]]`, else `--source <dir> holds no <file>: not a fedora-desktop checkout` (78): `helpers/pingbus/bundle.py`,
    `files/usr/local/bin/agent-bus`, `files/usr/local/sbin/agent-bus-install`,
    `files/usr/local/share/agent-bus/tuwunel.pin`, `files/usr/local/share/agent-bus/resolv.conf`,
    and the ten `KIT_FILES` under `files/opt/claude-yolo/optional/agent-bus/`:
    `agent-bus-claude settings.json plugin/pingbus/.claude-plugin/plugin.json plugin/pingbus/hooks/hooks.json plugin/pingbus/skills/pingbus/SKILL.md README.podman README.host README.lxc README.docker README.vm` (ten; the list is `KIT_FILES`, L48-50).
    S: a missing `SKILL.md` is 78, names the file, and nothing (no dnf, no directory) has happened.
05. `require_system_dir` for `<prefix>/usr/local/bin`, `.../sbin`, `/etc/systemd/system`: must be a
    directory (a symlink to one counts: Fedora merges sbin into bin), else
    `<path> is not a directory on this host` (1). They are never created or chmod-ed. S.
06. Make the work directory; read the pin (78 on any fault).
07. `install_packages`: `rpm -q --quiet <pkg>` for `python3 firewalld NetworkManager zstd curl jq tcpdump util-linux`; the missing ones in one `dnf install -y <missing...>` (stdout to
    stderr); `CHANGED\tpackages <missing joined by space>`.
08. `ensure_user`: `getent passwd agent-bus`; absent: `useradd --system --user-group --no-create-home --home-dir /var/lib/agent-bus --shell /usr/sbin/nologin --comment "Agent team bus homeserver" agent-bus`; `CHANGED\tsystem user agent-bus`.
09. Bus address check if not done in step 2.
10. Directories (`ensure_dir`, which creates with `install -d` and prints `CHANGED\tdirectory <path>`, or on a mode/owner mismatch chowns+chmods and prints `CHANGED\tdirectory <path> mode and owner`): `/var/lib/agent-bus` 0700 agent-bus; `/var/lib/agent-bus-install` 0700 root;
    `/usr/local/lib/agent-bus`, `/usr/local/share/agent-bus`, `.../kit` 0755 root.
11. Files (`put_file`, 5.9): `tuwunel.pin` and `resolv.conf` into `/usr/local/share/agent-bus/`
    0644 (labels `tuwunel.pin`, `resolver stub`).
12. `install_tuwunel` (5.8).
13. `build_zipapps`: for `agent-bus` then `pingbus`, `cd <source> && python3 -B -s -m helpers.pingbus.bundle --source <source> --out /usr/local/lib/agent-bus/<app>.pyz <app>`;
    stdout must start `BUNDLE-CHANGED ` (print `CHANGED\tzipapp <app>`) or `BUNDLE-UNCHANGED `
    (nothing); anything else: `the zipapp builder printed no marker for <app>: <output>` (1). The
    builder writes the 0755 file itself (atomic, only when bytes or mode differ); a builder
    failure leaks its status 1.
14. `put_symlink` `/usr/local/bin/pingbus` -> `../lib/agent-bus/pingbus.pyz` (label `pingbus command`); `put_file` the wrapper `agent-bus` into `/usr/local/bin` 0755 (label `agent-bus admin wrapper`); `put_file` the installer into `/usr/local/sbin` 0755 (label `agent-bus-install`);
    `put_file` `pingbus.pyz` as `/usr/local/share/agent-bus/kit/pingbus` 0755 (label `member kit pingbus`).
15. `install_kit`: `ensure_dir` 0755 for `kit/plugin`, `.../pingbus`, `.../.claude-plugin`,
    `.../hooks`, `.../skills`, `.../skills/pingbus`; `put_file` each of `KIT_FILES` (0644, the
    launcher `agent-bus-claude` 0755; label `member kit <file>`); `put_symlink`
    `/usr/local/bin/agent-bus-claude` -> `../share/agent-bus/kit/agent-bus-claude` (label
    `agent-bus-claude command`).
16. For each of `agent-bus-hs@.service agent-bus-backup@.service agent-bus-backup@.timer`:
    `put_file` from `<source>/files/etc/systemd/system/` to `/etc/systemd/system/` 0644 (label
    `unit <name>`). If any changed: `systemctl daemon-reload` once. Nothing is enabled, started or
    restarted by `software`. S.
17. If `--bus-address`: `ensure_bus_interface` (5.10).

Idempotency S (suite L456-462): a second identical run exits 0, runs **no** `curl`, `dnf`,
`useradd`, `daemon-reload`, `nmcli connection add|modify|up`, and prints nothing.

### 5.2 `team --team-file FILE` (L754-843)

01. Parse; require `--team-file`; root (77); Fedora (78); file regular and readable else
    `--team-file <f> is not a readable file` (64).
02. `require_software` (1): each of `/usr/local/lib/agent-bus/tuwunel`, `.../agent-bus.pyz`,
    `/usr/local/bin/agent-bus`, `/etc/systemd/system/agent-bus-hs@.service` must exist
    (`[[ -e ]]`) else `<path> is missing: run agent-bus-install software first`. S ("software" in
    the message).
03. `require_running firewalld.service NetworkManager.service`: `systemctl show -P ActiveState <unit>` must be `active` else `<unit> is <state>; the homeserver needs it running` (1). S.
04. Work directory. Team name: `jq -er '.team | strings' < file`; failure `team file <f>: no team name: <jq stderr>` (78); regex fail `team file <f>: team '<t>' is not a team name` (78).
05. `agent-bus render check [--previous=<dir>/team.json] < file > <work>/team.json` through the
    admin tool (`--previous` only when the installed `team.json` exists). A non-zero status `s`
    exits with `s` after `team file <f> refused (above)`; the admin tool's stderr already
    explained. S: malformed or wildcard listen addresses, CIDRs with host bits, bad prefix, no
    prefix, not JSON, a changed `server_name`, all 78 and "no team directory is made, no unit
    started".
06. `state` and `port` from the canonical JSON. `state != present`: `team <t> is state <s>: run agent-bus-install remove --team <t>` (78).
07. `assert_port_free` (L701-716): for each other `/var/lib/agent-bus/*/team.json` whose `.team`
    differs from this one and whose `.port` equals ours: `port <p> is team <other>'s` (78) (S names
    the other team). If our unit is not `active`: `ss -ltnH "sport = :<port>"` non-empty gives
    `port <p> is already in use on this host` (78).
08. `ip -j addr show` once; for each `listen[]` address find the one interface carrying it
    (`interface_of`): none `listen address <a> is on no interface of this host` (78, S names the
    address), several `listen address <a> is on more than one interface: <i1>, <i2>` (78).
09. `agent-bus render toml < team.json > tuwunel.toml`;
    `agent-bus render dropin --interface=<addr>=<iface>... < team.json > network.conf`.
10. Directories: `/var/lib/agent-bus` 0700 and `.../<team>` 0700 agent-bus; `db`, `backups`,
    `secrets` 0700 agent-bus; `/var/lib/agent-bus-install` and `.../<team>` 0700 root.
11. `put_file` `tuwunel.toml` -> `<team dir>/tuwunel.toml` 0640 `root:agent-bus` (label `team <t> tuwunel.toml`); change sets restart. `put_file` `team.json` 0640 `root:agent-bus` (label
    `team <t> team.json`); a change alone does not restart.
12. `ensure_shared_secret` (5.11).
13. `sync_firewall <team> <port> <allow_from...>` (5.12).
14. `ensure_dir /etc/systemd/system/agent-bus-hs@<team>.service.d` 0755 root; `put_file`
    `network.conf` 0644 (label `team <t> unit drop-in`); a change sets restart **and** reload.
15. `readlink -f /usr/local/lib/agent-bus/tuwunel` -> `put_file` to
    `/var/lib/agent-bus-install/<team>/binary` 0600 (label `team <t> binary <resolved path>`);
    change sets restart. `sha256sum` of `/etc/systemd/system/agent-bus-hs@.service` (hex only,
    newline) -> `.../unit-sha256` 0600 (label `team <t> base unit <hex>`); change sets restart.
    (So a new Tuwunel, or a changed base unit that `software` already reloaded, restarts the
    homeserver on the next `team` run, exactly once. S L627-649.)
16. If reload: `systemctl daemon-reload`.
17. `converge_units <team> <restart>`: for the hs service and the backup timer, `systemctl is-enabled --quiet <u>` else `systemctl enable <u>` (stdout to stderr) + `CHANGED\tenabled <u>`; the service: not `active` then `systemctl start` + `CHANGED\tstarted <u>`, else if
    restart `systemctl restart` + `CHANGED\trestarted <u>`; the timer: not active then
    `systemctl start` + `CHANGED\tstarted <u>`.
18. `wait_ready <hs unit> <port>` (5.13), then `assert_listening <port> <listen...>` (5.14), then
    `agent-bus bootstrap <team>` (its stdout passes through, status propagates).
19. Stamp, last: `running=$(readlink /usr/local/lib/agent-bus/tuwunel)` (a name like
    `tuwunel-1.9.3`); `self_sha` = sha256 of the running installer file (failure: `cannot read <path> to stamp it`, 1); `agent-bus render stamp --zipapp /usr/local/lib/agent-bus/agent-bus.pyz --tuwunel <version, "tuwunel-" stripped> --installer-sha256 <hex> < team.json > stamp.json`;
    `ensure_dir /var/lib/agent-bus-applied` 0755 root; `put_file` `stamp.json` ->
    `/var/lib/agent-bus-applied/<team>.json` 0644 (label `team <t> applied stamp`; world-readable on
    purpose: ccy-teams reads it without sudo, D65).

A stopped homeserver is started (S L609-614). Changing `allow_from` replaces exactly the old rule
(S L616-625). A changed server name is refused by step 5.

### 5.3 `remove --team TEAM [--purge]` (L856-889)

No Fedora check, no file validation. In order: if either unit is enabled (`systemctl is-enabled --quiet`) or active: `systemctl disable --now <hs> <timer>` (stdout to stderr) + `CHANGED\tstopped and disabled <hs> and <timer>`. `remove_firewall` (5.12). If the drop-in directory exists: `rm -rf` it, `systemctl daemon-reload`, `CHANGED\tremoved the <hs> drop-in`. `unstamp`: if
`/var/lib/agent-bus-applied/<team>.json` exists remove it + `CHANGED\tremoved the <team> applied stamp`. `--purge`: for `/var/lib/agent-bus/<team>` then `/var/lib/agent-bus-install/<team>`,
if it exists `rm -rf` + `CHANGED\tpurged <path>`. Without `--purge` the data stays (S). A second
remove exits 0 and prints nothing (S). Removing a team that never existed is a silent success
(deliberate: the play removes absent entries every run).

### 5.4 `backup-now --team TEAM` (L961-1007)

Root; `require_team_dir` (`team <t> is not installed here (no <dir>/team.json)`, 78); `require_running <hs>` (1); `wait_ready` first (a Tuwunel that has not installed its SIGUSR2 handler dies of the
signal; S L751-754 pins "poll before kill"); work directory; `before = latest_backup(meta)`;
`systemctl kill --kill-whom=main --signal=SIGUSR2 <hs>`; poll up to 60 times, 1 s apart, for a
higher number (else `no new backup in <meta> after 60 s (is admin_signal_execute set?)`, 1);
tar `-C <dir> -cf state.tar -- secrets team.json [registry.json] [room_id]` (the optional two
only if present); `ensure_dir /var/lib/agent-bus-install/<team>` and `.../backups` 0700 root;
install the tar as `.../backups/agent-bus-state-<n>.tar` 0600 root; delete any
`agent-bus-state-<id>.tar` whose `<meta>/<id>` no longer exists (Tuwunel prunes old backups);
`CHANGED\tbackup <n> of team <team>`. `latest_backup` is the highest directory name in `meta/`
matching `^[1-9][0-9]*$`, or 0.

### 5.5 `backups --team TEAM` (L1011-1032)

Root; `require_team_dir` (78). Ids = names in `<dir>/backups/meta/` matching `^[1-9][0-9]*$` that
also have `/var/lib/agent-bus-install/<team>/backups/agent-bus-state-<id>.tar`; none: exit 0,
no output. Sorted numerically ascending; one line each `BACKUP\t<id>\t<YYYY-MM-DD HH:MM UTC>` (the
tar's mtime, `date -u`). A database backup without its state tar is not listed (restore would refuse
it, S L770-773).

### 5.6 `restore --team TEAM --backup ID` (L1034-1095)

1. Parse; team option; both options; `--backup '<id>' is not a backup number` (64); root;
   `require_team_dir` (78); `require_software` (1).
2. `team <t> has no database backup <id>` (78) if `<dir>/backups/meta/<id>` is missing;
   `team <t> has no state tar for backup <id>` (78) if the tar is missing;
   `check_state_tar` failure: its problems on stderr then `state tar <path> refused (above); nothing changed` (78). S: unknown id 78; tampered tars 78 with "homeserver not stopped, nothing
   run" (5.7).
3. Work directory; stage `<work>/state` (0700); extract with `tar -C stage --no-same-owner --no-same-permissions -xf`; every directory 0700, every file 0600, whole stage `chown -R agent-bus:agent-bus`. `sandbox_properties` (reads `[Service]` of the installed
   `agent-bus-hs@.service`, skips comments and `ExecStart`, replaces `%i` by the team, any other
   `%` is `agent-bus-hs@.service: unsupported specifier in '<line>'` 1) becomes one
   `--property=<line>` each, plus `--property=IPAddressAllow=127.0.0.1/32 ::1/128` and
   `--property=SocketBindAllow=tcp:<port>` (port from `team.json`).
4. **From here the homeserver is down and state is replaced; nothing is rolled back.**
   `systemctl stop <hs>`; remove `team.json registry.json room_id`; remove `secrets/`; move the
   staged `secrets/` (and `registry.json`, `room_id` if staged) into place; install the staged
   `team.json` 0640 `root:agent-bus`; `unstamp` (may print `CHANGED\tremoved the <team> applied stamp`).
5. `systemd-run --unit=agent-bus-restore-<team>.service --collect <properties...> -- /usr/local/lib/agent-bus/tuwunel --restore-backup <id>` (stdout to stderr). `--collect` so a
   failed run leaves no failed unit blocking the name (S L804-812). `wait_ready` on that unit
   (failure names `agent-bus-restore-<team>.service is ...`), `systemctl stop` it, `systemctl start <hs>`, `CHANGED\trestored team <team> from backup <id>`, `wait_ready <hs>`, `assert_listening`,
   `agent-bus bootstrap <team>`.
6. S: properties from the sandbox appear (ten pinned), no `ExecStart` property, state files absent
   from the backup are removed, restored secret 0600, `secrets/` 0700, `team.json` 0640.

### 5.7 `check_state_tar` (L911-937)

Opens the file as an uncompressed tar. Every member must be one of: a regular file named exactly
`team.json`, `registry.json` or `room_id`; a regular file below `secrets/` (path parts all
non-empty, not `.`, not `..`, at least two parts); the directory `secrets` or a directory below it.
Anything else (other names, absolute names, `..`, symlinks, hardlinks, devices) is the problem
`<name!r>: not one of the state files`. Any member with `mode & 0o7000` (setuid, setgid, sticky):
`<name!r>: mode <octal> carries a setuid, setgid or sticky bit`. Required: members named
`team.json` and `secrets`: `<need> is missing`. Problems printed one per line to stderr; non-zero
exit if any; a corrupt tar is a Python traceback (also non-zero, mapped to 78). Suite S: stray
member (names `etc/passwd`), setuid file (says `setuid`), `secrets/../../x`, a symlink, a tar
without `secrets`: each 78, homeserver untouched, `secrets` intact.

### 5.8 `install_tuwunel` (L354-383)

`uname -m`: `x86_64` -> asset arch `x86_64-v1`, `aarch64` -> `aarch64-v8`, else `no pinned Tuwunel build for architecture <m>` (78). Installed binary `/usr/local/lib/agent-bus/tuwunel-<ver without v>`; record `<binary>.asset-sha256`. If both exist and the record equals the pinned sha:
`say "Tuwunel <ver> is installed from the pinned asset; nothing to download"` (stderr). Else:
`say "downloading <asset>"` where asset is `<ver>-release-all-<arch>-linux-gnu-tuwunel.zst`;
`curl --fail --silent --show-error --location --proto =https --tlsv1.2 --max-time 600 -o <work>/<asset> https://github.com/matrix-construct/tuwunel/releases/download/<ver>/<asset>`;
sha256 of the downloaded file must equal the pin else `<asset>: sha256 <got> is not the pinned <pin>; nothing installed` (1); `zstd -q -d -f -o <work>/tuwunel -- <work>/<asset>`; write the pin
hash + newline to `<work>/asset-sha256`; `put_file` the binary 0755 (label `tuwunel <ver>`) and the
record 0644 (label `tuwunel <ver> asset record`). Then always `put_symlink` `tuwunel` ->
`tuwunel-<ver>` (label `tuwunel now <ver>`). S: downloaded once; a changed pinned hash downloads
again and installs the new bytes; a bad hash installs nothing and leaves no `tuwunel` link.

### 5.9 `put_file`, `put_symlink`, `ensure_dir`, `require_system_dir`

- `ensure_dir PATH MODE OWNER GROUP`: a symlink or a non-directory in the way: `<path> exists and is not a directory` (1). Missing: create with that mode and ownership, `CHANGED\tdirectory <path-without-prefix>`. Present with a different `mode owner group` (compared as `stat -c '%a %U %G'`): `chown owner:group` then `chmod mode`, `CHANGED\tdirectory <path> mode and owner`.
- `put_file LABEL SRC DEST MODE OWNER GROUP`: `<dest> is a symlink; refusing to write through it`
  (1). If DEST is a regular file with identical bytes **and** identical mode/owner/group: do
  nothing. Else write SRC to a sibling temp `.<name>.agent-bus-install.<pid>` with the mode and
  ownership, rename over DEST, `CHANGED\t<label>`, set the "changed" flag the callers read.
- `put_symlink LABEL TARGET LINK`: already a symlink with that target: nothing. A non-symlink
  in the way: `<link> exists and is not a symlink` (1). Else make the link atomically (temp +
  rename) and `CHANGED\t<label>`. The target is stored as given (relative).

### 5.10 `ensure_bus_interface ADDRESS` (L398-432)

IPv6 if the address contains `:` (settings `ipv4.method disabled ipv4.addresses "" ipv6.method manual ipv6.addresses <a>/128`), else IPv4 (`ipv4.method manual ipv4.addresses <a>/32 ipv6.method disabled ipv6.addresses ""`). `nmcli -t -f NAME connection show` has no line equal to `agentbus0`:
`nmcli connection add type dummy ifname agentbus0 con-name agentbus0 connection.autoconnect yes <settings>` (stdout to stderr), `CHANGED\tagentbus0 dummy connection with <addr>`. Exists:
`nmcli -g ipv4.addresses,ipv6.addresses connection show agentbus0` compared with the wanted
`<v4>\n<v6>` (command substitution strips trailing newlines on both sides); different: `nmcli connection modify agentbus0 <settings>`, `nmcli connection up agentbus0`, `CHANGED\tagentbus0 address now <addr>`, return. Finally `nmcli -g GENERAL.STATE connection show agentbus0`; not
`activated`: `nmcli connection up agentbus0`, `CHANGED\tagentbus0 up`. S: add with the address, then
up; a second run touches nothing; a new address modifies.

### 5.11 `ensure_shared_secret` (L718-728)

`<team dir>/secrets/registration_shared_secret`: if it exists (any file, any mode) nothing.
Else 64 bytes from `/dev/urandom` as 128 lowercase hex chars (anything else: `could not read 64 random bytes`, 1), **no trailing newline**, written through a 0600 file in the work directory,
installed 0600 `agent-bus:agent-bus`, temp removed, `CHANGED\tregistration shared secret created`.
S: 0600, 128 chars, 128 bytes, kept across runs, never in output.

### 5.12 Firewall (L534-636)

Record: `/var/lib/agent-bus-install/<team>/firewalld.rules`, 0600 root, lines `zone<TAB>rule`.

- `zone_of_cidr <cidr>`: `ip -j route get <cidr minus /prefix>`, `.[0].dev` (none: `no route to allow_from <cidr>`, 1); `firewall-cmd --get-zone-of-interface=<dev>` (stderr merged): success
  prints the zone; output `no zone` falls back to `firewall-cmd --get-default-zone`; any other
  failure `firewall-cmd --get-zone-of-interface=<dev> failed: <out>` (1).
- `rich_rule`: `rule family="ipv4|ipv6" source address="<cidr>" port port="<port>" protocol="tcp" accept` (family ipv6 iff the CIDR contains `:`).
- `fw_add zone rule`, for store in permanent then runtime (`--permanent` only for the first):
  `firewall-cmd [--permanent] --zone=<z> --query-rich-rule=<rule>` (stdout discarded); if it
  fails, `firewall-cmd [--permanent] --zone=<z> --add-rich-rule=<rule>` (stdout to stderr) and
  `CHANGED\tfirewalld <permanent|runtime> <zone>: <rule>`. `fw_remove` is the mirror with
  `--remove-rich-rule` and `CHANGED\tfirewalld <store> <zone>: removed <rule>`, only if the query
  succeeds.
- `sync_firewall`: wanted = `zone\trule` per CIDR (zone resolved first, so a route failure
  aborts before any change). **Before any firewall-cmd mutation** write the record as the sorted
  unique union of the old record and the wanted lines (0600, only if different), so a failure
  part-way leaves every rule this run may have added in the record `remove` reads (S L680-694). Then
  remove record lines not wanted (`fw_remove`), `fw_add` each wanted, write the final record
  (the wanted lines in order, or empty) only if different.
- `remove_firewall`: no record: nothing; else `fw_remove` every line, then delete the record.
  Nothing outside the record is ever removed. No `--reload`.

### 5.13 `wait_ready UNIT PORT` (L651-671)

Up to 60 tries: `curl --fail --silent --show-error --noproxy * --max-time 2 http://127.0.0.1:<port>/_tuwunel/server_version`; success `say "<unit> answers on 127.0.0.1:<port>"`, return. On a failed try read `systemctl show -P ActiveState <unit>`; not
`active`: print the journal (`print_unit_journal`: `say "the last 40 journal lines of <unit>:"`,
`journalctl --unit <unit> --lines 40 --no-pager --output short-iso` to stderr, and on its failure
`say "journalctl could not read the journal of <unit>"`), then die 1: `readiness: <unit> is <state> (<SubState>), not active: it did not start (an unknown or malformed tuwunel.toml key is one cause). See: systemctl status --no-pager <unit>; journalctl -u <unit> --no-pager`. Active
but silent: `sleep 1`. After 60: the journal, then `readiness: <unit> is active but <url> did not answer in 60 s: <curl output>` (1). The verdict is the unit's state, never a journal scan.

### 5.14 `bound_addresses` and `assert_listening` (L674-698)

`ss -ltnH "sport = :<port>"`; for each row the 4th field (local address:port): strip the port
(`%:*`), a leading `[`, a trailing `]`, and a `%zone` suffix; sorted unique. Expected = sorted
unique of `127.0.0.1` plus the listen addresses. Not equal: `port <p> is bound on [<got joined by space>], not exactly [<want>]` (1). S names the unexpected address and then `bootstrap` must not
run.

### 5.15 `check --team TEAM` (L1110-1156)

Root; `require_team_dir` (78). Prints in this order (`fact name verdict value`; FAIL sets a flag,
all facts still print, then exit 1):
`ActiveState` (`ok` iff `active`), `UnitFileState` (`ok` iff `enabled`), then `info` for `User IPAddressDeny IPAddressAllow RestrictAddressFamilies IPAccounting SocketBindDeny SocketBindAllow`
(`systemctl show -P <prop> <hs>`, newlines in a multi-valued property turned into spaces, S
`any any`), `drop-in` (`ok` iff `/etc/systemd/system/agent-bus-hs@<team>.service.d/network.conf` is
a file; value = the path), `server_name` (`ok` iff the name ends `.internal`), `listen` (`ok` iff
the bound set equals 127.0.0.1 + listen; value `port <p> bound on: <addresses space-joined>`),
`firewalld` (for each recorded rule `firewall-cmd --permanent --zone=<z> --query-rich-rule=<rule>`;
`ok` iff none absent; value `<n> rule(s) recorded, <m> missing from the permanent configuration`),
`binary` (`info`, `readlink -f /usr/local/lib/agent-bus/tuwunel`). Changes nothing (S L737-738).

## 6. Files read and written

| Path (under `<prefix>` in test)                                                                     | Mode, owner                                    | By             | Notes                                  |
| --------------------------------------------------------------------------------------------------- | ---------------------------------------------- | -------------- | -------------------------------------- |
| `/var/lib/agent-bus`                                                                                | 0700 agent-bus                                 | software, team | parent of all team state               |
| `/var/lib/agent-bus/<team>/{db,backups,secrets}`                                                    | 0700 agent-bus                                 | team           |                                        |
| `.../<team>/tuwunel.toml`                                                                           | 0640 root:agent-bus                            | team           | rendered; change restarts              |
| `.../<team>/team.json`                                                                              | 0640 root:agent-bus                            | team, restore  | canonical; read by every other command |
| `.../<team>/secrets/registration_shared_secret`                                                     | 0600 agent-bus                                 | team           | 128 hex, no newline                    |
| `.../<team>/backups/meta/<n>`                                                                       | Tuwunel's                                      | read           | numbered backup dirs                   |
| `.../<team>/{registry.json,room_id}`                                                                | agent-bus (admin tool's)                       | read, restore  | optional state                         |
| `/var/lib/agent-bus-install`, `.../<team>`                                                          | 0700 root                                      | software, team | root's own records                     |
| `.../<team>/firewalld.rules`                                                                        | 0600 root                                      | team, remove   | `zone<TAB>rule`                        |
| `.../<team>/binary`, `.../<team>/unit-sha256`                                                       | 0600 root                                      | team           | restart triggers                       |
| `.../<team>/backups/agent-bus-state-<n>.tar`                                                        | 0600 root (dir 0700)                           | backup-now     | pruned with Tuwunel's                  |
| `/var/lib/agent-bus-applied`, `.../<team>.json`                                                     | 0755 / 0644 root                               | team, unstamp  | world-readable stamp (no secret)       |
| `/usr/local/lib/agent-bus/{tuwunel-<v>,tuwunel-<v>.asset-sha256,tuwunel,agent-bus.pyz,pingbus.pyz}` | 0755/0644/link/0755/0755 root                  | software       |                                        |
| `/usr/local/share/agent-bus/{tuwunel.pin,resolv.conf}`                                              | 0644 root                                      | software       |                                        |
| `/usr/local/share/agent-bus/kit/...`                                                                | dirs 0755, files 0644 (launcher, pingbus 0755) | software       |                                        |
| `/usr/local/bin/{agent-bus,pingbus,agent-bus-claude}`                                               | 0755 / links                                   | software       |                                        |
| `/usr/local/sbin/agent-bus-install`                                                                 | 0755 root                                      | software       | itself                                 |
| `/etc/systemd/system/agent-bus-{hs@.service,backup@.service,backup@.timer}`                         | 0644 root                                      | software       |                                        |
| `/etc/systemd/system/agent-bus-hs@<team>.service.d/network.conf`                                    | dir 0755, file 0644 root                       | team           | rendered drop-in                       |
| `/etc/os-release`                                                                                   | read                                           | software, team | `ID=` only                             |

Never created or chmod-ed: `/usr/local/bin`, `/usr/local/sbin`, `/etc/systemd/system` (S).

## 7. External commands (every one becomes an `execx.Cmd`)

`oracle` = the suite stubs it, so the port must still run it as a child (otherwise the suite's
stub state diverges); `native` = the suite runs the real tool, and the port replaces it with Go
standard library.

| Tool                                                                                                                                                                                    | argv (as run)                                                                                                                                                                                                                                                                                                        | Fate / disposition                                                                                               |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `rpm`                                                                                                                                                                                   | `rpm -q --quiet <pkg>`                                                                                                                                                                                                                                                                                               | execx (oracle)                                                                                                   |
| `dnf`                                                                                                                                                                                   | `dnf install -y <pkgs...>` (stdout to stderr)                                                                                                                                                                                                                                                                        | execx (oracle)                                                                                                   |
| `getent`                                                                                                                                                                                | `getent passwd agent-bus`                                                                                                                                                                                                                                                                                            | execx (oracle); `os/user` would break the suite's stub state                                                     |
| `useradd`                                                                                                                                                                               | `useradd --system --user-group --no-create-home --home-dir /var/lib/agent-bus --shell /usr/sbin/nologin --comment "Agent team bus homeserver" agent-bus`                                                                                                                                                             | execx (oracle)                                                                                                   |
| `uname`                                                                                                                                                                                 | `uname -m`                                                                                                                                                                                                                                                                                                           | execx (oracle): the stub pins x86_64 on any CI host                                                              |
| `curl`                                                                                                                                                                                  | download and readiness argv in 5.8, 5.13                                                                                                                                                                                                                                                                             | execx (oracle) in v1 (D3)                                                                                        |
| `zstd`                                                                                                                                                                                  | `zstd -q -d -f -o <out> -- <in>`                                                                                                                                                                                                                                                                                     | execx (oracle); no stdlib zstd                                                                                   |
| `python3`                                                                                                                                                                               | `python3 -B -s -m helpers.pingbus.bundle --source <s> --out <o> <app>` with `Dir=<s>`; and two `python3 -I -c` snippets (8.2)                                                                                                                                                                                        | bundle: execx, keep (D2). Snippets: native (absorbed)                                                            |
| `agent-bus`                                                                                                                                                                             | `agent-bus render check [--previous=<f>]` stdin team file; `render toml`; `render dropin [--interface=A=I]...`; `render stamp --zipapp <p> --tuwunel <v> --installer-sha256 <h>`; `bootstrap <team>`                                                                                                                 | execx (oracle: the stub runs the real `render`)                                                                  |
| `systemctl`                                                                                                                                                                             | `show -P ActiveState\|SubState\|UnitFileState\|<prop> <unit>`, `is-enabled --quiet <u>`, `enable <u>`, `disable --now <hs> <timer>`, `start <u>`, `restart <u>`, `stop <u>`, `daemon-reload`, `kill --kill-whom=main --signal=SIGUSR2 <hs>`                                                                          | execx (oracle)                                                                                                   |
| `systemd-run`                                                                                                                                                                           | `systemd-run --unit=agent-bus-restore-<team>.service --collect --property=... -- /usr/local/lib/agent-bus/tuwunel --restore-backup <id>`                                                                                                                                                                             | execx (oracle)                                                                                                   |
| `journalctl`                                                                                                                                                                            | `journalctl --unit <u> --lines 40 --no-pager --output short-iso`                                                                                                                                                                                                                                                     | execx (oracle)                                                                                                   |
| `firewall-cmd`                                                                                                                                                                          | `--get-zone-of-interface=<dev>`, `--get-default-zone`, `[--permanent] --zone=<z> --query-rich-rule=<r>\|--add-rich-rule=<r>\|--remove-rich-rule=<r>`                                                                                                                                                                 | execx (oracle)                                                                                                   |
| `nmcli`                                                                                                                                                                                 | `-t -f NAME connection show`; `connection add type dummy ifname agentbus0 con-name agentbus0 connection.autoconnect yes <settings>`; `-g ipv4.addresses,ipv6.addresses connection show agentbus0`; `connection modify agentbus0 <settings>`; `connection up agentbus0`; `-g GENERAL.STATE connection show agentbus0` | execx (oracle)                                                                                                   |
| `ip`                                                                                                                                                                                    | `ip -j addr show`; `ip -j route get <addr>`                                                                                                                                                                                                                                                                          | execx (oracle); JSON parsed with `encoding/json`                                                                 |
| `ss`                                                                                                                                                                                    | `ss -ltnH "sport = :<port>"`                                                                                                                                                                                                                                                                                         | execx (oracle)                                                                                                   |
| `sleep`                                                                                                                                                                                 | `sleep 1`                                                                                                                                                                                                                                                                                                            | `time.Sleep` behind an injectable sleeper                                                                        |
| `jq`, `install`, `mv`, `cmp`, `stat`, `chown`, `chmod`, `rm`, `ln`, `readlink`, `mktemp`, `od`, `tr`, `sort`, `tar`, `find`, `sha256sum`, `date`, `awk`, `grep`, `cat`, `dirname`, `id` | (as in the script)                                                                                                                                                                                                                                                                                                   | native: `encoding/json`, `os`, `io/fs`, `archive/tar`, `crypto/sha256`, `crypto/rand`, `time`, `sort`, `os.Root` |

`jq` stays in `PACKAGES` (the suite regex and other consumers use it) even though the Go binary
no longer needs it; dropping it is a later cleanup, not part of the port.

## 8. Python and other helper seams

### 8.1 Called from the installer today

| Call                                           | What it does                                                            | Port decision                                                                                                                                                                                                                                                                                                          |
| ---------------------------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `python3 -I -c` `valid_bus_address` (L190-201) | canonical, concrete IP check                                            | **Absorb** with `net/netip`: `ParseAddr`, `addr.String() == text`, refuse unspecified, loopback, link-local (unicast and multicast), multicast, **and zone and IPv4-mapped forms** (B2). No python3 dependency for this check any more (also removes the B11 ordering wrinkle).                                         |
| `python3 -I -c` `check_state_tar` (L911-937)   | state tar validation                                                    | **Absorb** with `archive/tar`, and validate and extract in one pass (B5). Accept only `TypeReg`/`TypeRegA` and `TypeDir`; refuse every other typeflag.                                                                                                                                                                |
| `python3 -B -s -m helpers.pingbus.bundle ...`  | reproducible zipapp builder; prints `BUNDLE-CHANGED/UNCHANGED <path>`   | **Keep calling it.** Two other consumers need byte-identical output: `declare.py` (the software digest hashes the same build, `declare.py:385`) and `scripts/` tests. Reimplementing a zip writer in Go would have to reproduce Python's bytes forever. Revisit only with the agent_bus family port (P2 in INVENTORY). |
| `agent-bus render check\|toml\|dropin\|stamp`  | validation and rendering (`helpers/agent_bus/{cli,teamfile,render}.py`) | **Keep calling it** (D2). It is the admin tool and the suite's stub runs the real one. This is the second reason `python3` stays a hard dependency of the host install.                                                                                                                                                |
| `agent-bus bootstrap <team>`                   | account operations as the `agent-bus` user                              | **Keep.** Account and registry logic is the admin tool's, and the only code that talks to the homeserver.                                                                                                                                                                                                              |

### 8.2 Not called by the installer, and what happens to them

| Helper                                                                                                   | Relation                                                                                                                                | Port decision                                                                                                                                                        |
| -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `helpers/agent_bus/declare.py`                                                                           | called from Go by `internal/declare` (python3 -P -m ...); owns host_vars editing and the applied/outdated verdict (`declared_software`) | **Out of scope for this port.** Only the installer digest it hashes is coupled (8.3).                                                                                |
| `helpers/agent_bus/element.py`                                                                           | Element profile for `play-agent-bus-element.yml`; no installer relation                                                                 | Out of scope.                                                                                                                                                        |
| `helpers/agent_bus/{admin,checkout,registry,render,teamfile,cli}.py`, `helpers/agent_bus_lxc/members.py` | the admin tool and seat code                                                                                                            | Unchanged. They are the rest of the agent_bus family (INVENTORY P2). After this port the family is: Go installer, Go ccy-teams, Python admin tool + declare + seats. |

### 8.3 The one coupling that bites: the installer digest

`teamfile.software_digest(zipapp, tuwunel_version, installer_sha256)` is stamped by `render stamp`
with `--installer-sha256 = sha256(the installer file that ran)` (L837-839), and
`declare.declared_software` recomputes it as `sha256(<checkout>/files/usr/local/sbin/agent-bus-install)`
(`declare.py:371,384`). A mismatch is what ccy-teams shows as "out of date" (D65). Replacing the
bash file by a Go binary breaks this recipe in three places: `declare.INSTALLER_FILE`,
`tests/helpers/agent_bus/test_declare.py:550-557`, and the file the installer hashes.
Options, decision D1 in section 14:

- **A. Source digest (recommended).** The "installer identity" is a hash over the build inputs, not
  the output: sha256 of the sorted `(path, sha256)` list of `src-go/go.mod`, `src-go/go.sum`,
  `src-go/cmd/agent-bus-install/**` and every `src-go/internal/**` package the binary imports
  (`go list -deps`). The play computes it with the helper that owns the definition (`declare.py`
  today, Go later) and bakes it in with `-ldflags "-X main.SourceDigest=<hex>"`; the installer
  stamps that value; `declare` recomputes it from the checkout. It keeps "the checkout moved on,
  the play has not run" visible, needs no toolchain at status time, and survives a Go toolchain
  update only through the play's rebuild.
- **B. Binary digest.** `declare` runs a reproducible `go build` into a temp file and hashes it.
  Exact, but ccy-teams status then needs the Go toolchain and a build per status call, and any
  flag difference with the play's build changes the hash.
- **C. Installed-binary digest.** Hash `/usr/local/sbin/agent-bus-install`. Cheap, but it measures
  what is installed, not what the checkout would install, so the D65 signal is lost.

## 9. Idempotency and state

- Every mutation is conditional on a difference (bytes+mode+owner, link target, unit state,
  address). A converged host prints no `CHANGED`, runs no install/useradd/dnf/daemon-reload/
  firewall mutation and never restarts the homeserver (S: `software` L456-462, `team` L595-607).
- The only unconditional work: the readiness poll and listening check of `team` (a failure here
  is how an unhealthy homeserver shows), `bootstrap` (its own idempotency), stamp rewrite only on
  change, `nmcli ... show` probes.
- Restart rule: tuwunel.toml, drop-in, installed-binary record or base-unit record changed =>
  restart (not `team.json` alone); an inactive service is started instead.
- State machine of a team: absent -> (`team`) -> built+stamped -> (`remove`) -> removed (data
  kept, stamp gone) -> (`remove --purge`) -> gone. `restore` removes the stamp so ccy-teams shows
  the team out of date until the play applies it again.
- Partial failure leaves: a pre-written firewall record (so remove cleans), no stamp (stamp is
  last), orphan temp files (B9), a stopped homeserver after a failed restore (B8).
- No lock: two concurrent mutating runs are not excluded (B16).

## 10. The oracle suite

### 10.1 How it works

`scripts/test-agent-bus-install.bash` copies a minimal source tree (the repository's `helpers/pingbus`,
`helpers/agent_bus`, units, resolver stub, wrapper, kit and the **installer file itself** into
`<source>/files/usr/local/sbin/`), writes a test pin whose x86_64 hash is the fixture asset's, and
runs the installer under `AGENT_BUS_INSTALL_TEST_PREFIX=<root>` with stubs first on PATH:
`systemctl journalctl firewall-cmd nmcli curl ip ss rpm dnf getent useradd zstd uname systemd-run agent-bus`. Stubs keep state under a per-case `STUB_DIR` and log each call as `<name> <args>` to
`$STUB_DIR/log`; the `agent-bus` stub runs the real `python3 -m helpers.agent_bus.cli render ...`
and logs/answers `bootstrap`. `set -e` is not used; `check LABEL WANT GOT` counts. Helpers:
`says` (grep -E), `count`, `mode`, `same`, `exists`, `yes_if`.

### 10.2 Cases to port sections (AI = this suite; static check calls / runtime assertions)

| AI section (L)                               | Asserts                                                                                                                                                                                                                                                                                                                                          | Spec section    | Checks (runtime) |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------- | ---------------- |
| argument parsing (364-410)                   | no args 64; `--help` 0 + `Usage:`; unknown cmd/option, missing value, missing required 64; bad team name 64; non-numeric backup 64; 7 bad bus addresses 64 and zero child calls; non-Fedora 78 naming os-release; non-root 77                                                                                                                    | 1, 2.2, 5.1.3   | 21               |
| software (413-466)                           | modes, tuwunel under its version + link, both pyz built and the admin zipapp runs, pingbus link, wrapper/installer/kit installed (each kit file + mode), units 0644, resolver, pin, `/var/lib/agent-bus` 0700, one download, dnf/useradd/daemon-reload/nmcli add+up argv, stdout only CHANGED; second run quiet; new address modifies            | 5.1, 5.8-5.10   | 54               |
| software: OS dirs (468-483)                  | sbin symlink accepted, lands through it, link kept; missing system dir fails 1, names it, not created                                                                                                                                                                                                                                            | 5.1.5           | 6                |
| software: incomplete kit (485-494)           | 78 naming the file, before anything installed, no dnf                                                                                                                                                                                                                                                                                            | 5.1.4           | 4                |
| software: bad hash (496-504)                 | 1, names sha256, nothing under that version, no link                                                                                                                                                                                                                                                                                             | 5.8             | 4                |
| software: re-hash (506-517)                  | changed pin downloads again, installs new bytes                                                                                                                                                                                                                                                                                                  | 5.8             | 3                |
| team: refused files (520-548)                | 8 refusal shapes each 78 + no dir + no unit start (24 checks) and the unlisted-address refusal naming the address                                                                                                                                                                                                                                | 5.2.5, 5.2.8    | 26               |
| team: first install (550-593)                | dirs 0700, toml content+0640, canonical team.json, secret 0600/128/no newline, drop-in content, rich rule permanent+runtime in the route's zone, enable/start counts, readiness polled, bootstrap, CHANGED-only stdout, no secret leak, stamp 0644 in a 0755 dir with the `config` digest equal to `teamfile.config_digest`, stamp has no secret | 5.2, 5.11, 5.12 | 28               |
| team: nothing changed (595-607)              | no start/restart/stop, no CHANGED, no firewall mutation, no reload, secret kept, readiness still checked, bootstrap's CHANGED passes through                                                                                                                                                                                                     | 9               | 8                |
| team: stopped homeserver (609-614)           | started once, CHANGED                                                                                                                                                                                                                                                                                                                            | 5.2.17          | 2                |
| team: allow_from changed (616-625)           | restart once, reload, old rule removed, new added, permanent zone holds only the new                                                                                                                                                                                                                                                             | 5.12            | 7                |
| team: new binary (627-634)                   | restart on a new version, CHANGED                                                                                                                                                                                                                                                                                                                | 5.2.15          | 3                |
| team: base unit changed (636-650)            | software reloads and restarts nothing; next team run restarts once; the one after does not                                                                                                                                                                                                                                                       | 5.2.15          | 5                |
| team: server name (652-657)                  | 78, installed team.json kept                                                                                                                                                                                                                                                                                                                     | 5.2.5           | 2                |
| team: unexpected bound address (659-665)     | 1, names the address, bootstrap does not run                                                                                                                                                                                                                                                                                                     | 5.14            | 3                |
| team: unit that fails to start (667-678)     | 1; `agent-bus-hs@beta.service is failed \(auto-restart\)`; `journalctl -u agent-bus-hs@beta.service`; journal text on stderr; journalctl argv; no bootstrap                                                                                                                                                                                      | 5.13            | 6                |
| team: firewall add fails (680-694)           | 1; first rule present and in the record; later `remove` deletes it (permanent and runtime)                                                                                                                                                                                                                                                       | 5.12            | 6                |
| team: port held (696-705)                    | second team same port 78 naming the first                                                                                                                                                                                                                                                                                                        | 5.2.7           | 3                |
| team: needs firewalld/software (707-720)     | 1 naming firewalld; 1 naming software                                                                                                                                                                                                                                                                                                            | 5.2.2-5.2.3     | 4                |
| check (723-744)                              | exit 0; listen/firewalld ok lines; IPAddressAllow line; multi-line property on one line; no stray line; changes nothing; missing rule: exit 1 + FAIL; team puts it back                                                                                                                                                                          | 5.15, 5.12      | 11               |
| backup-now (746-763)                         | signal argv; polled before kill; tar 0600 in root's 0700 dir, none in agent-bus's backups/, holds the secret and team.json; CHANGED `backup 1 of team alpha`                                                                                                                                                                                     | 5.4             | 9                |
| backups (765-775)                            | `BACKUP\t1\t<UTC time>` one line; backup without tar not listed; unknown team 78                                                                                                                                                                                                                                                                 | 5.5             | 5                |
| restore (777-802)                            | unknown id 78; stop first; one-off run as agent-bus with `--restore-backup 1`, `--collect`, 10 sandbox properties, no ExecStart; stop once; start hs; bootstrap; absent files removed; secret 0600; `secrets/` 0700; `team.json` 0640                                                                                                            | 5.6             | 23               |
| restore: failed one-off then retry (804-812) | 1 naming the unit; retry succeeds                                                                                                                                                                                                                                                                                                                | 5.6.5           | 3                |
| restore: tampered tar (814-857)              | 5 shapes each 78, hs not stopped, nothing run (15); names `etc/passwd`, `setuid`; secret survives                                                                                                                                                                                                                                                | 5.7             | 18               |
| remove (859-873)                             | disable --now argv; drop-in gone; rich rule gone; data stays; stamp gone; CHANGED; second remove silent; `--purge` removes data                                                                                                                                                                                                                  | 5.3             | 10               |
| **Total**                                    |                                                                                                                                                                                                                                                                                                                                                  |                 | **274**          |

Gaps the port must close with new cases (the suite never exercises them): `remove` stdout texts
beyond "CHANGED"; `software --bus-address` IPv6; `check` exit code 1 with FAIL on a non-firewall
fact; `restore` failing between stop and start (B8); `wait_ready` timeout (60 s) path; the
`--name=value` forms for every option; `help` and `-h`; a relative `--source`; `put_file`
refusing a symlink destination; `ensure_dir` mode/owner correction message; the lock, if D4 adds one.

### 10.3 Other tests that touch the installer

- `scripts/test-ccy-teams-command.bash` (TC): **fakes the installer completely** (a bash script at
  `$BIN/agent-bus-install`, L206-216, answering `backups`, `backup-now`, `restore`, `remove`; any
  other subcommand exits 97). It pins the argv ccy-teams sends (`sudo -n /usr/local/sbin/agent-bus-install ...`, L467, L848, L1170, L1212). It is not an oracle for
  this port, but the port must keep these four argv shapes byte-identical. No change needed.
- `tests/helpers/agent_bus/test_member_docs.py`: (a) `installer_synopsis` runs `bash <installer> --help` and parses command lines of the form `  command --opt ...`; every `agent-bus-install`
  invocation in the docs and READMEs must use a listed command and only its listed options (b)
  `test_the_installer_ships_every_readme` regex-parses `readonly KIT_FILES=(...)` out of the **bash
  source** (c) `test_the_installer_kit_dir_is_the_one_bundles_name` regex-parses `readonly SHARE_DIR=` and `KIT_DIR=` (must equal `admin.KIT_DIR`). All three read bash and must be
  rewritten in the cut-over commit: (a) runs the built binary with `--help`; (b) and (c) read the
  Go constants (`kitFiles`, `shareDir`, `kitDir` in `internal/businstall`) by regex, or move to
  a Go test that reads `files/opt/claude-yolo/optional/agent-bus` and compares with
  `protocol.HANDLE_TYPES` listed in the test.
- `tests/helpers/agent_bus/test_declare.py:550-557` and `declare.INSTALLER_FILE`: digest recipe (8.3).
- `tests/helpers/agent_bus/{ccy_teams_play.py,test_cli.py,test_teamfile.py}`: mention the installer
  in comments or build stamps without running it.
- `CLAUDE/Plan/00161-agent-team-bus-matrix/_bus-address.inc.bash`, `deploy_check.py`: mirror the
  bus-address rule for the plan's own scripts; keep consistent with B2.
- `scripts/qa-all.bash` L1141-1151: runs the suite as a hard gate named `agent-bus-install`.
  `scripts/qa-changed.bash` has no mapping for it today; the cut-over adds `agent-bus-install: scripts/test-agent-bus-install.bash` beside the ccy-teams line (L338) so a `src-go/` change in
  the installer's dependency set runs it.

## 11. Latent bugs

Disposition: **FIX** = change deliberately, with a new or adjusted suite case and the reason in the
commit; **KEEP** = reproduce for parity (note stays in the Go source); **VERIFY** = needs a host
measurement before choosing (put the probe in the port plan's triage script).

| #   | Bug                                                                                                                                                                                                                                                                                                                                                              | Disposition                                                                                                                                                                  |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | `src-go/internal/sudo/sudo.go:18` says the sudoers rule covers `agent-bus-install`; it does not (section 0, 2.3). The installer is reached only through password sudo, so a ccy-teams run with no terminal and a lapsed credential fails with sudo's words.                                                                                                      | FIX (comment and the AgentNotes sentence, in the port commit)                                                                                                                |
| B2  | `valid_bus_address` accepts an IPv6 literal with a zone id (`2001:db8::1%eth0`: confirmed, Python 3.11 round-trips it), which then reaches `nmcli` as `2001:db8::1%eth0/128`; acceptance of IPv4-mapped forms is Python-version dependent.                                                                                                                       | FIX (refuse zones and mapped forms; suite gains two bad values)                                                                                                              |
| B3  | Exit codes leak from unguarded children under `set -e`: curl exits 77 (bad CA file) and 78 (remote file not found) collide with `EXIT_NOT_ROOT` and `EXIT_CONFIG`, and ccy-teams maps them to the wrong kind; `jq`, `nmcli`, `install`, `dnf` leak 2-100 with no message.                                                                                        | FIX: every child failure is exit 1 with a message naming the step, except the admin tool's `render check` and `bootstrap` whose 64/69/70/75/77/78 are the contract           |
| B4  | `$TMPDIR` is honoured by `mktemp -d` and the restore stage holds the team's secrets (PR3).                                                                                                                                                                                                                                                                       | FIX (work dir under `/var/lib/agent-bus-install`)                                                                                                                            |
| B5  | `check_state_tar` (Python `tarfile`) and the extraction (`tar -x`) are two different parsers over the same bytes: a header the two read differently (long names, pax attributes) is a validation bypass. The tar is root-written, so exposure needs a writer into root's backups directory, but the design intends defence in depth.                             | FIX (one pass in Go, no `tar -x`, no `find`/`chmod`/`chown -R` subprocesses)                                                                                                 |
| B6  | `backup-now` tars the live, agent-bus-owned tree; a symlink the homeserver planted in `secrets/` is stored as a symlink, the tar is written and listed by `backups`, and `restore` refuses it only later.                                                                                                                                                        | FIX (refuse at backup time: `backup: <path> is a <kind>, not a regular file`, 1)                                                                                             |
| B7  | `ensure_shared_secret` returns when the file merely exists: a wrong mode, owner or size is never repaired.                                                                                                                                                                                                                                                       | FIX (mode 0600 and owner repaired, `CHANGED\tregistration shared secret mode and owner`; content is never touched)                                                           |
| B8  | `restore` is not atomic: after `systemctl stop` any failure (one-off run, readiness, listening, bootstrap) leaves the homeserver stopped (or running without bootstrap) with half-replaced state and no hint. The stamp is already gone, so ccy-teams shows it out of date.                                                                                      | KEEP behaviour; FIX the message: on any failure after the stop, print `the homeserver of <team> is stopped and its state is replaced: run restore again, or team to rebuild` |
| B9  | `put_file` leaves `.<name>.agent-bus-install.<pid>` behind when `install` or `mv` fails.                                                                                                                                                                                                                                                                         | FIX (remove the temp on error)                                                                                                                                               |
| B10 | `assert_port_free` runs `jq` over every other team's `team.json`; a malformed one aborts the run with jq's status and no message.                                                                                                                                                                                                                                | FIX (`<file>: not readable JSON: <err>`, 78)                                                                                                                                 |
| B11 | `software` validates `--bus-address` before the root check only if `python3` is already installed; on a host without python3 the order differs (validated after dnf).                                                                                                                                                                                            | FIX (always up front, now that the check is native)                                                                                                                          |
| B12 | `zone_of_cidr` treats the interface-has-no-zone case by exit status plus the exact text `no zone`; whether `firewall-cmd --get-zone-of-interface` exits non-zero in that case on this Fedora is unmeasured. If it exits 0 the zone `no zone` is used for the rule.                                                                                               | FIXED in the port (text authoritative), pinned by the suite case "Go port: B12"; the host probe confirms the real tool                                                            |
| B13 | `ensure_bus_interface` compares `nmcli -g ipv4.addresses,ipv6.addresses` against the wanted text. `-g` is terse mode, which escapes `:` in values, so an IPv6 bus address would never compare equal and every run would `modify` + `up` and print `CHANGED`. Suspected, unmeasured. Several configured addresses print comma-separated and are rewritten to one. | FIXED in the port (`netprobe.AddressesMatch` normalises), pinned by the suite case "Go port: B13"; the host probe confirms the real tool                                                  |
| B14 | `check` verifies firewall rules in the permanent configuration only, not the runtime one, though `team` adds both.                                                                                                                                                                                                                                               | KEEP (documented behaviour; add a runtime query as `info` later)                                                                                                             |
| B15 | `help` is accepted as a command though the usage does not list it; `--help` ignores trailing arguments; `-h` after a subcommand is `unexpected argument`.                                                                                                                                                                                                        | KEEP                                                                                                                                                                         |
| B16 | No lock: `team`, `remove`, `restore` and the daily `backup-now` timer can run at once (play during a backup); `latest_backup` and the record file are racy.                                                                                                                                                                                                      | KEEP in the port; decision D4 (one `flock` on `/var/lib/agent-bus-install/.lock`)                                                                                            |
| B17 | The bundler runs as root with `cwd=<source>` and an inherited Python environment (PR4).                                                                                                                                                                                                                                                                          | FIX (clean environment); cwd stays                                                                                                                                           |
| B18 | A `remove` of a team that never existed, or whose data directory is already gone, is a silent success.                                                                                                                                                                                                                                                           | KEEP (the play relies on it)                                                                                                                                                 |
| B19 | `restore` takes `team.json` from the tar without running `render check` on it; the tar is root's and validated, but its content may predate a change in what `render check` accepts.                                                                                                                                                                             | KEEP (validate after restore through `bootstrap`; note only)                                                                                                                 |
| B20 | `put_file`'s "unchanged" test reads the whole destination every run (`cmp`); fine at these sizes, but for the 100+ MB Tuwunel binary it is skipped only because the asset record is trusted without re-hashing the installed binary, so a corrupted binary with a matching record is never detected.                                                             | KEEP (record is the contract); note                                                                                                                                          |
| B21 | `fw_remove` is called inside a `while read < record` loop; a child that read stdin would eat the record. `firewall-cmd` does not today.                                                                                                                                                                                                                          | FIX for free (the Go port reads the record fully first)                                                                                                                      |
| B22 | `useradd` is run whenever `getent passwd agent-bus` exits non-zero, whatever the status. | FIX (deviation): only status 2 (no such entry) means absent; any other status is an error, so a broken name service stops the run instead of attempting `useradd` |
| B23 | `software` installs the Tuwunel pin and the resolver stub before Tuwunel itself (bash steps 11 then 12). | KEEP the files and the results; only the order of the `CHANGED` lines on stdout differs (the Go installer installs the pin and the stub after Tuwunel) |
| B24 | A team's own `team` field is printed unescaped when another team holds the port (`port N is team X's`). | FIX (deviation): the other team's name is quoted (`strconv.Quote`) before it reaches stderr |
| B25 | `curl` read root's `~/.curlrc` and the proxy variables for the Tuwunel download. | FIX (deviation): `curl -q`; the sha256 against the pin was the real check, this removes a variable from the trust story |

## 12. Proposed Go design

Built on the shared packages of `src-go/CLAUDE.md`: `execx` (every external call and its `Fake`),
`term` (exit codes, diagnostics), `agentbus` (exit kinds; its `Client` is the *caller's* view and
stays), `systemd` (only `HomeserverState` today; this port adds the unit-level calls it needs
there), `marker` (the `CHANGED`/`CHECK`/`BACKUP` line shapes), `sudo` (unchanged).

### 12.1 Packages

New, each behind an interface with a fake, test file first:

| Package               | Responsibility                                                                                                                                                                                                                                                                                                                   |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/businstall` | the flows: `App` (dispatch, usage, option parsing, exit codes), and one file per command: `software.go`, `team.go`, `remove.go`, `backup.go` (backup-now, backups, restore), `check.go`; `usage.txt` embedded; `config.go` (the path layout, production vs test).                                                                |
| `internal/fsx`        | the file primitives with one implementation each: `EnsureDir`, `PutFile`, `PutSymlink`, `RequireSystemDir`, atomic temp+rename, no-follow checks via `os.Root`, owner/mode comparison, and a `Changes` sink that prints `CHANGED\t...`. An `Owners` resolver maps `root`/`agent-bus` to uid/gid (test config: the running user). |
| `internal/tarstate`   | `Write` (state tar from a root) and `Extract` (validate-and-extract in one pass into a stage), with the problem list text of 5.7.                                                                                                                                                                                                |
| `internal/pin`        | the `tuwunel.pin` parser with the 78-class errors of 2.2.                                                                                                                                                                                                                                                                        |
| `internal/busaddr`    | `Valid(text)` (B2 included). Reusable by ccy-teams, which today asks `declare.py` for the same answer (`declare.Client.CheckAddress`): one rule, one place.                                                                                                                                                                      |
| `internal/netprobe`   | pure parsers for `ip -j addr show`, `ip -j route get`, `ss -ltnH` (5.14), `nmcli` outputs.                                                                                                                                                                                                                                       |
| `internal/firewalld`  | rich-rule construction, zone resolution, the record file and the sync algorithm of 5.12, over an `execx.Runner`.                                                                                                                                                                                                                 |

Not new: no package for `curl` (execx, D3), `systemctl` beyond additions to `internal/systemd`
(`Show(prop,unit)`, `IsEnabled`, `Enable`, `DisableNow`, `Start`, `Restart`, `Stop`, `Kill`, `DaemonReload`,
`RunTransient`), `journalctl` (`Journal(unit,n)`).

### 12.2 The binary

`src-go/cmd/agent-bus-install/main.go`: thin, like `ccy-teams`:

```go
func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }
```

`run` builds the production `businstall.Config` (or, only under the test build tag, the
prefixed one, 12.4) and returns an exit code. Unlike ccy-teams it has **no `RootDir`**: the
checkout arrives as `--source`. It has a `SourceDigest` string variable (`-X main.SourceDigest`,
8.3 option A) that `team` stamps, and prints it for `agent-bus-install --version`
(new, additive, so the play and `qa-deployed-drift` can compare without running a flow; not
in the usage's command list so `installer_synopsis` is unaffected, or listed if the docs test
wants it).

Package test requirements follow `qa-go.bash`: every package has tests, `-race`, the coverage
floor, `golangci-lint` strict, no discarded errors. Flow tests use `execx.Fake` plus an in-memory
or `t.TempDir()` `fsx` root (no root needed because `Owners` is injectable).

### 12.3 Behaviour that must be reproduced mechanically

- Exit-code mapping through `term` (`ExitOK`, `ExitFailure`=1, `ExitUsage`=64) plus two installer
  codes `ExitNotRoot`=77 and `ExitConfig`=78 added to `term`; an `ExitError` from the admin tool's
  `render check`/`bootstrap` passes its code through.
- The `die` shape `agent-bus-install: <message>` on stderr; `say` the same prefix.
- Stdout is a writer owned by `App` and only `changed()`, the three line families, `--help` and the
  admin tool's passthrough write to it.
- The message strings in section 5 are the contract for ccy-teams display and the suite regexes.

### 12.4 Parity mode: the suite drives the Go binary

The suite is the oracle and stays bash (a bash suite that stubs `systemctl` and friends is the
most direct statement of the contract, and `scripts/test-ccy-teams-command.bash` already drives
a Go binary the same way, L60-78).

1. The suite builds the binary like TC does, with the test hook compiled in:
   `go build -trimpath -buildvcs=false -tags agentbustest -o "$SCRATCH/agent-bus-install" ./cmd/agent-bus-install` run in `src-go/` (build failure is `check "the Go binary builds"` and
   an exit).
2. `-tags agentbustest` compiles `testmode.go`, which reads `AGENT_BUS_INSTALL_TEST_PREFIX` exactly
   as L62-77 do (root check skipped, PATH kept, admin tool from PATH, owners = current user).
   Without the tag the variable is not read at all and `testmode_stub.go` returns the production
   config (PR1). The play builds without the tag.
3. `TOOL` becomes that binary. The source-tree copy step drops `cp "$TOOL" "$SOURCE/files/usr/local/sbin/"`
   (a Go install has no installer file in the tree) and `software`'s needed-file list drops
   `files/usr/local/sbin/agent-bus-install` (D5); "the installer installs itself" then asserts
   the file at `<root>/usr/local/sbin/agent-bus-install` is mode 755 **and byte-identical to
   `$TOOL`**. The OS-dirs case asserts the same through the sbin symlink.
4. The non-root case copies the binary (`install -m 755 "$TOOL" "$OUTSIDE/agent-bus-install"`)
   and runs it with `env -u AGENT_BUS_INSTALL_TEST_PREFIX`; with the production-semantics binary
   (no tag) the non-root refusal is now structural. Build both: the tagged one for the flows and
   an untagged one for the non-root and "variable ignored" cases (new case: the untagged binary
   run as root with the variable set refuses nothing differently from a plain run).
5. Slow paths get an injectable sleeper: the `sleep 1` retries (readiness, backup wait) are real
   in bash; in the Go unit tests they are a fake. The suite's stubs make the real binary fail on
   the first try or succeed at once, as today, so the bash suite keeps real time.
6. Everything else in the suite stays as written; the changes are 3 and 4, the added gap cases
   of 10.2, and the B-list FIX cases. The suite stays a hard gate in `qa-all.bash`; `qa-changed.bash`
   maps `src-go/` changes in `./cmd/agent-bus-install`'s dependency set to it (L338-358 pattern).
7. The Go unit tests additionally run the **real** `helpers/pingbus/bundle.py` and `agent-bus render`
   against a temp tree (as `internal/declare/integration_test.go` does for `declare.py`) so a
   change to those files reruns the Go gate (src-go/CLAUDE.md "Parity oracles").

## 13. Cut-over

### 13.1 Build and install (reading how `play-claude-yolo.yml` builds ccy-teams)

`play-claude-yolo.yml` (play-level `become: false`) imports `play-golang.yml` first, then per
binary: a `command` task `go build -trimpath -buildvcs=false [-ldflags ...] -o ~/.cache/ccy-go-build/<name> ./cmd/<name>` with `chdir: <root_dir>/src-go`, `GOTOOLCHAIN: local`,
`GOFLAGS: -mod=readonly`, `GOCACHE`, `HOME`, `changed_when: false`; then an `ansible.builtin.copy`
`remote_src: true` of the scratch binary to the destination, owner/group/mode explicit (that task
is the one that reports a change). `play-agent-bus.yml` is `become: true` at play level and is
the one that runs the installer, so the new tasks go there with these deliberate differences:

1. `import_playbook: play-golang.yml` at the top (same single source of truth for the toolchain).
2. The build task runs with `become: false` (as the login user, like the ccy plays), not as root:
   root must not execute a toolchain over a user-writable tree and must not write the user's Go
   cache. Flags as for ccy-teams plus `-ldflags "-X main.SourceDigest=<hex>"` (D1 option A) and
   **no** `-tags agentbustest`.
3. A `copy` (root, `remote_src: true`) installs the scratch binary at
   `/usr/local/sbin/agent-bus-install` owner `root`, group `root`, mode `0755`: the **same path and
   mode** as today, so the unit `agent-bus-backup@.service` ExecStart, the sudo argv that ccy-teams
   builds (`sudo.InstallTool`), and `docs/agent-bus.md` need no change.
4. `agent_bus_installer` becomes `/usr/local/sbin/agent-bus-install` (the root-owned copy): the play
   no longer executes the installer as a user-writable checkout file as root (2.1 item 5). The
   other half of that item stays: the bundler is checkout Python run as root, and the admin
   wrapper is installed from the checkout and run as root. `software --source <checkout>` keeps its self-install step, which is then a no-op in the play (bytes equal) and the
   real install on the "other projects" route (D5).
5. The existing `software`/`remove`/`team` tasks and their `changed_when: ^CHANGED\t` are untouched;
   the sudoers task is untouched.
6. Order on a first run: build, install binary, then `software`. On a host where the new binary
   is not yet installed the first `copy` creates it; there is no chicken-and-egg because the
   play, not the installer, places the first copy.
7. The retired file: delete `files/usr/local/sbin/agent-bus-install` in the cut-over commit; update
   `scripts/qa-deployed-drift.bash` (remove the pair at L233, or replace it by comparing
   `agent-bus-install --version` with the digest the checkout would produce); `declare.py`
   `INSTALLER_FILE` and its tests per D1; `docs/agent-bus.md` L197 and the kit READMEs per D5;
   the Plan 00161 `deploy.bash`/`acceptance.bash` `INSTALLER=` lines (they are plan-local and
   historical; add a note to their plan rather than rewrite, or point them at the installed path).
8. `docs/ccy-changelog.md`/CCY version: not touched (no `files/var/local/claude-yolo/` change).

The duplication this adds (a third build+copy pair) is a signal to extract a shared task file for
"build a src-go command and install it"; that refactor is separate and optional (D6).

### 13.2 The sudoers rule

Unchanged by the port and unaffected by it (section 2.3): same file, same two regexes, same
wrapper path and mode. ccy-teams keeps calling the installer by the absolute path through the
cached sudo credential. Do **not** extend the rule to the installer as part of the port.

### 13.3 Order of work (phases, each ending with the suite green)

1. `internal/fsx`, `busaddr`, `pin`, `tarstate`, `netprobe` with unit tests (pure, no execx).
2. `internal/businstall` read-only commands first (`check`, `backups`), then `remove`, `backup-now`,
   `restore`, `team`, `software`; at each step the suite (built against the Go binary) runs the
   cases for that command; the bash installer stays in place and the suite can run against either
   (`TOOL=` selects), which is the parity check.
3. Cut-over commit: play build+install tasks, delete the bash file, update the three
   Python tests, `qa-deployed-drift`, `qa-changed` mapping, docs. The same commit is the one
   that stops the suite from copying the bash file.
4. Host verification by `meta-deploy.bash` with the play, then the VERIFY probes (B12, B13).

## 14. Decisions for the owner

| #   | Question                                                                                                      | Recommendation                                                                                                                  |
| --- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| D1  | What identity does the stamp carry now that the installer is a binary (8.3)?                                  | A: source digest baked in with ldflags, recomputed by `declare`.                                                                |
| D2  | Keep `render`/`bootstrap`/bundler as Python for now?                                                          | Yes; they are the admin tool and the byte-reproducible zipapp (INVENTORY P2 later).                                             |
| D3  | Keep `curl` as a child (suite stubs it) or use `net/http`?                                                    | Keep in v1; revisit when the suite can run a local HTTP server.                                                                 |
| D4  | Add a `flock` so a play and the backup timer cannot interleave (B16)?                                         | Yes, but as its own change after parity; blocking with a bounded wait.                                                          |
| D5  | The "run from a pinned clone" route for other projects: Go toolchain on that host, or ship a prebuilt binary? | Document `go build` into a temp path then `sudo <bin> software --source <clone>`; drop the needed-file check on the old script. |
| D6  | Extract the repeated build+install tasks into a shared task file?                                             | Later, once a third Go binary is deployed by the plays.                                                                         |
| D7  | B2, B3, B5, B6, B7, B9, B10, B11 are deliberate behaviour changes.                                            | Approve as a set; each lands with its suite case and a commit reason.                                                           |
