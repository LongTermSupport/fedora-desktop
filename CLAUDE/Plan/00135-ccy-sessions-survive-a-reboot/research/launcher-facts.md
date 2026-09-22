# CCY session internals — read-only recon for issue #44 (warn before reboot, restore after)

Scope: what ALREADY exists. No edits made.

---

## 1. The `ccy` launcher — tmux start, socket, naming, hook points, on-disk state

**Launcher**: `/workspace/files/var/local/claude-yolo/claude-yolo` (~3000+ lines, mode 0600 in repo,
deployed 0755). `CCY_VERSION="3.58.3"` at `claude-yolo:17` — **mandatory bump on any edit**
(`.claude/rules/ccy-version-bump.md`, pre-commit hook enforces).

**Libraries** loaded in order at `claude-yolo:24-64`:
`CCY_LIBS=(common token-management ssh-handling network-management dockerfile-custom docker-health tmux-session)`.
`common.bash` sources `common-pure.bash` itself. `CCY_HASH` (`claude-yolo:105-107`) is computed over the
script + lib set, so a lib edit without a version bump triggers the "DEVELOPER ERROR" banner at
`claude-yolo:418-435`.

**How the tmux session starts** — `ccy_tmux_insulate()`, `lib/tmux-session.bash:248-328`.
The launcher re-executes ITSELF inside tmux before the first prompt. Final exec at
`tmux-session.bash:322-327`:

```
exec systemd-run --user --scope --quiet --collect \
    --unit "ccy-tmux-$$" --description "CCY tmux session $name" \
    -- tmux -L "$CCY_TMUX_SOCKET" \
    new-session -d -s "$name" -- bash -c "$hold_on_failure" ccy-tmux "$@" \; \
    set-hook -g client-attached "$(ccy_tmux_single_attach_hook)" \; \
    attach-session -t "=$name"
```

- **Socket**: `CCY_TMUX_SOCKET="ccy"` (`tmux-session.bash:29`) — i.e. `tmux -L ccy`, a dedicated
  server, never the user's default one. Every call goes through `ccy_tmux()` (`:36-38`).
- **Session naming**: `CCY_TMUX_SESSION_PREFIX` (`:33`, defaults `ccy`) → `ccy-<project>`,
  `ccy-<project>-2`, … (`ccy_tmux_next_name`, `:72-81`). Host `cc` wrapper sets the prefix to `cc`
  (`files/var/local/claude-code/cc:60`) so `cc-<project>` sessions share the same server.
- **Project name** comes from `get_project_name` (defined in `lib/common.bash`), parent-project format
  — `claude-yolo:174`.
- **cgroup**: the server lives in a `ccy-tmux-<pid>.scope` under `systemd --user`, deliberately out of
  the terminal tab's `*-spawn-*.scope`. `ccy_tmux_insulate` REFUSES to run inside a tab-scoped tmux
  server (`tmux-session.bash:262-279`).
- **One-terminal rule**: server-wide `client-attached` hook (`ccy_tmux_single_attach_hook`, `:94-97`)
  detaches any second client.

**Where the insulation is invoked**: `claude-yolo:891-902`

```
if [ "$HEADLESS_MODE" != true ] && [ "$CREATE_TOKEN_MODE" != true ] && [ "$UPDATE_TOKEN_MODE" != true ]; then
    ccy_tmux_insulate "$PROJECT_NAME" "$SCRIPT_DIR/$(basename "$0")" "$@" || exit 1
    ccy_tmux_banner || exit 1
fi
```

All the "exit without a session" modes (`--export-token`, `--connect`, `--custom`, `--custom-docker`,
`--top`, `--list-tokens`, `--prevent`, `--version`, `--help`) return BEFORE this point
(`claude-yolo:862-889` and earlier).

**Natural hook points**

- *Session start*: immediately after the `new-session` succeeds — but note `ccy_tmux_insulate` `exec`s,
  so it never returns. Two clean options: (a) write the state record just before the `exec` at
  `tmux-session.bash:313-322` (name, `$PWD`, prefix, replayable argv are all in hand there); or
  (b) inside the tmux trampoline, i.e. extend `$hold_on_failure` (`:317`).
- *Session exit*: the trampoline at `:317` is the only place that runs after the wrapped command
  returns (`"$@"; rc=$?; …`) — the natural place to delete the state record. Alternatively a
  tmux `session-closed` hook on the `ccy` server, which would also catch kills from `ccy-sessions`
  Ctrl-X (`files/home/.local/bin/ccy-sessions:75`).

**Per-session state on disk: NONE today.** There is no `~/.local/state/ccy` or equivalent. What exists:

| Path                                                        | Written by                                                                | Contents                                                                                                                         |
| ----------------------------------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `.claude/ccy/.last-launch.conf` (per project, mode 0600)    | `save_launch_config`, `claude-yolo:454-478`; called at `claude-yolo:2880` | `SAVED_CONFIG_VERSION`, `SAVED_CCY_VERSION`, `SAVED_CCY_HASH`, `LAST_TOKEN`, `LAST_SSH_KEYS`, `LAST_NETWORK`, `LAST_LAUNCH_DATE` |
| `~/.claude-tokens/ccy/` (`CCY_ROOT`, `claude-yolo:180-182`) | launcher                                                                  | `tokens/`, `projects/` (mkdir at `:816`), `usage-cache`                                                                          |
| `~/.cache/claude-yolo-update-checks` (`claude-yolo:188`)    | launcher                                                                  | per-image daily update stamps                                                                                                    |

`load_launch_config` (`claude-yolo:371-451`) invalidates the config on schema/version/hash mismatch and
deletes it. **This is the closest existing precedent for a restore record**, and its version/hash
guarding is the pattern to copy — but it is per-project and single-slot, not per-session.

The repo's established XDG state convention is `$XDG_STATE_HOME/fedora-desktop` — see
`helpers/play_ledger/ledger.py:48-81` (a tested helper that resolves and validates it), and
`~/.local/state/vmtest-bridge/<slug>/` (`files/home/.local/bin/vmtest-bridge-watcher:44`).

---

## 2. `lib/tmux-session.bash` — what is reusable vs interactive-only

**Reusable, non-interactive** (safe for a restore service):

| Function                             | Line    | Returns                                                                                                                                |
| ------------------------------------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `ccy_tmux()`                         | 36      | wrapper for `tmux -L ccy "$@"`                                                                                                         |
| `ccy_tmux_list()`                    | 43-54   | `"<name> <attached-count> <session_path>"` per line; **"no server running" prints nothing and returns 0**, any other failure returns 1 |
| `ccy_tmux_project_sessions(project)` | 60-69   | same rows filtered to `$PWD` + prefix match (matches on DIRECTORY, not name — deliberate, see comment `:56-59`)                        |
| `ccy_tmux_next_name(project)`        | 72-81   | first free session name                                                                                                                |
| `ccy_tmux_is_detached(name)`         | 85-89   | 0 detached, 1 attached, **2 = listing failed**                                                                                         |
| `ccy_tmux_single_attach_hook()`      | 94-97   | the hook string                                                                                                                        |
| `ccy_tmux_row(name, attached, dir)`  | 171-177 | one aligned display row                                                                                                                |
| `ccy_tmux_banner()`                  | 332-339 | one stderr line                                                                                                                        |

So a restore service CAN reuse `ccy_tmux_list` / `ccy_tmux_next_name` / `ccy_tmux_is_detached` directly.
`ccy_tmux_list` already returns the **working directory** (`#{session_path}`) — that is the one field a
restorer needs and it is already exposed.

**Interactive-only** (need fzf + a tty, unusable from a systemd unit):
`ccy_tmux_offer` (129-165), `ccy_tmux_confirm` (182-189), `ccy_tmux_header` (193-200),
`ccy_tmux_pick` (209-236, hard-fails without `fzf`), `ccy_tmux_attach` (103-123, attaches the CURRENT
terminal), `ccy_tmux_insulate` (248-328, `exec`s / `exit`s — never returns on the happy path).

Dependency to note: the library needs `print_error` from `common-pure.bash`, "always loaded first"
(`tmux-session.bash:26`).

---

## 3. `ccy-sessions` — it EXISTS

`/workspace/files/home/.local/bin/ccy-sessions` (152 lines). Deployed by
`playbooks/imports/play-claude-yolo.yml:435-441` to `/home/{{ user_login }}/.local/bin/ccy-sessions`,
mode 0755.

**It has NO subcommands.** Its entire CLI is (`ccy-sessions:30-40`):

- `ccy-sessions` → the fzf picker
- `ccy-sessions -h|--help` → usage (printed by re-reading its own header, `:26-28`)
- anything else → `exit 64` with "unknown option"

In-picker keys: Enter attach, **Ctrl-X** end (with confirm, `:72-80`), **Ctrl-N** new ccy session here
(`exec`s the launcher, `:91-93`; only if `git rev-parse --is-inside-work-tree`), Esc/q quit. Bounded
retry loop, `MAX_TRIES=3` (`:25`, `:147-151`).

It refuses to run inside a CCY container (`:46-53`, detects `/workspace`) and requires a tty (`:42-45`).

=> **Adding subcommands here means changing the arg parser at `:30-40`**, which currently treats any
argument as an error. Existing style would want each new verb to keep working without a tty only if it
genuinely does not attach.

---

## 4. `--supervise`, `--continue`, and which args are one-shot

**Parsing**: one flat `for arg in "$@"` loop, `claude-yolo:523-641`, with `NEXT_IS_*` booleans for
value-taking flags and a `SAW_DOUBLE_DASH` gate (`:521`, `:524-529`) — everything after `--` goes raw
into `CLAUDE_ARGS`. Unrecognised `--flag`s fall through to `CLAUDE_ARGS` (`:638-640`) and are then
validated against `claude --help` at `:673-740`, with did-you-mean suggestions.

**`--supervise`** (`:594-596` → `SUPERVISE_MODE=true`; acted on at `:3040-3062`). Mutually exclusive
with `--no-supervise` (`:3052-3054`). It only ARMS a supervisor that already runs UNARMED by default
whenever the project ships `.claude/ccy/claude-supervise.py`. A host export of `CCY_CLAUDE_WRAPPER`
outranks it; per-project `.claude/ccy/ccy.env` is sourced by `entrypoint.sh`.

**`--continue` is NOT a ccy flag.** It is Claude Code's own and falls through to `CLAUDE_ARGS`
(`claude-yolo:638-640`), validated against `claude --help`. Same for `--resume`, `--model`. Confirmed by
`docs/ccy.md:490-492`.

**Full argv passthrough on re-exec**: `ccy_tmux_insulate "$PROJECT_NAME" "$SCRIPT_DIR/$(basename "$0")" "$@"`
(`claude-yolo:900`) replays the ENTIRE original argv inside tmux. A restorer that re-ran `"$@"` verbatim
would inherit that behaviour — including the one-shot flags below.

**One-shot / wrong-to-replay on restore:**

| Flag                          | Why                                                                                               |
| ----------------------------- | ------------------------------------------------------------------------------------------------- |
| `--rebuild`, `--rebuild=MODE` | rebuilds the image every restore; minutes of work, already done                                   |
| `--create-token`              | exits without a session (`CREATE_TOKEN_MODE`, excluded from insulation at `:899`)                 |
| `--update-token[=NAME]`       | same — exits without a session                                                                    |
| `--list-tokens`               | prints and exits                                                                                  |
| `--export-token [NAME]`       | prints/exits, `claude-yolo:862-871`                                                               |
| `--custom`, `--custom-docker` | interactive Dockerfile editors, exit at `:880-889`                                                |
| `--top`                       | container manager, not a session                                                                  |
| `--prevent`                   | **destructive**: writes `never` to `.claude/ccy/allowed-hostnames`, disabling ccy for the project |
| `--connect [NET]`             | acts on an already-running container, exits at `:873-877`                                         |
| `--debug`                     | opens the interactive debug-layer chooser                                                         |
| `--headless`                  | no terminal to restore into; excluded from insulation at `:899`                                   |
| `--prompt "text"`             | replays a stale instruction into a fresh session                                                  |
| positional `"task"` text      | same problem — it is a first-message, not a setting                                               |
| `--disable-custom-docker`     | a one-run escape hatch for a broken Dockerfile                                                    |

**Safe/desirable to replay**: `--token NAME`, `--ssh-key PATH`, `--ssh-agent` (agent socket will differ
after reboot — needs thought), `--no-ssh`, `--github-443`, `--network NET`, `--no-network`, `--engine`,
`--supervise` / `--no-supervise`, and anything after `--`.

Note `--token` / `--ssh-key` / `--network` are ALREADY persisted per project in `.last-launch.conf`
(`claude-yolo:2880`), so a restore could lean on the quick-launch path (`claude-yolo:911-915`) rather
than replaying argv at all.

---

## 5. systemd `--user` units — the established pattern

**Linger is owned by one play**: `playbooks/imports/play-systemd-user-tweaks.yml:23-50` —
`getent passwd` → `loginctl enable-linger {{ user_login }}` with `creates: /var/lib/systemd/linger/{{ user_login }}`
→ then an explicit `ansible.builtin.systemd: name: user@<uid>.service, state: started` to close the
enable-linger async race. Tagged `always, systemd, linger`. Every other play can assume the user manager
is up because this runs in `playbook-main.yml` with `scope: general`.

(A second, lab-local copy exists at `playbooks/imports/optional/common/play-vm-test-lab.yml:99-105`.)

**Unit file location in the repo**: `files/home/.config/systemd/user/`. Existing units:
`container-watch.service`, `container-watch.timer`, `vmtest-nightly.{service,timer}`,
`vmtest-bridge@.{service,path}`, `vmtest-bridge-heartbeat@.{service,timer}`,
`host-health{,-collect}.{service,timer}.j2` (the `.j2` ones are templated).

**Concrete example to copy** — `playbooks/imports/optional/common/play-container-watch.yml`:

1. `file: path=/home/{{ user_login }}/.config/systemd/user state=directory mode=0755` owned by the user.
2. `copy:` the unit(s) from `{{ root_dir }}/files/home/.config/systemd/user/{{ item }}`, `mode: "0644"`,
   with an **explicit `loop:` list** (never a glob).
3. `getent passwd` → `assert` the uid resolves (the play has a long comment on why
   `| default(1000)` was a bug).
4. `become_user: "{{ user_login }}"` + `ansible.builtin.systemd: scope: user, enabled: true, state: started, daemon_reload: true`, with
   `environment: XDG_RUNTIME_DIR: "/run/user/{{ ansible_facts['getent_passwd'][user_login][1] }}"`.
   Unconditional — the enable is its own probe, no `is-system-running` guard, no skip-and-warn.

Unit style (`files/home/.config/systemd/user/container-watch.service`): `Description=`,
`Documentation=https://github.com/LongTermSupport/fedora-desktop`, `After=graphical-session.target`,
`Type=oneshot`, `ExecStart=%h/.local/bin/<command> <subcommand>` — i.e. the unit calls a user-bin
command, it does not inline logic.

---

## 6. Test harness

**Two families, both wired into `scripts/qa-all.bash`:**

*(a) Bash unit tests* — `scripts/test-*.bash`, run by `qa-all.bash` at `:317-700`. ccy-specific ones:
`test-ccy-rootless-guard.bash`, `test-ccy-token-mode.bash`, `test-ccy-ssh-handling.bash`,
`test-ccy-ssh-probe.bash`, `test-ccy-selinux-verdict.bash`, `test-ccy-gpu-device.bash`,
`test-ccy-host-hostname.bash`.

Wiring pattern in `qa-all.bash` (e.g. `:351-358`) — a comment block explaining what bug the suite exists
for, then:

```
foo_out=""
if ! foo_out="$(bash "$SCRIPT_DIR/test-foo.bash" 2>&1)"; then
    qa_hard_gate_failed foo "foo unit tests failed" "$foo_out"
fi
foo_summary=$(qa_gate_case_count "$foo_out")
qa_pass_line foo "$foo_summary"
```

Test style (`scripts/test-ccy-rootless-guard.bash` is the cleanest model):
`set -uo pipefail` — **deliberately NOT `-e`**, so every case runs and the summary is complete;
source the lib from THIS repo (`$REPO_ROOT/files/var/local/claude-yolo/lib/...`) not `/var/local`,
with `# shellcheck source-path=SCRIPTDIR` + `# shellcheck source=` directives; assert the function is
defined (`declare -F`) before testing; a `check()` helper printing `PASS`/`FAIL` with counters;
`=== section ===` headers; negative controls emphasised.

*(b) Python helper tests* — `tests/helpers/<pkg>/test_*.py`, stdlib `unittest` only (no pytest, no
venv), run by `scripts/qa-helper-tests.bash` (invoked at `qa-all.bash:264`). Mirrors `helpers/<pkg>/`.

**There is NO existing test for `tmux-session.bash`.** Nothing under `tests/` or `scripts/` greps for
`tmux`. The functions listed in §2 as reusable are pure-enough to test the `test-ccy-rootless-guard.bash`
way (stub `ccy_tmux` / feed `ccy_tmux_list` output as a string) — `ccy_tmux_next_name`,
`ccy_tmux_is_detached`, `ccy_tmux_project_sessions` and `ccy_tmux_row` all take text and return text.

---

## 7. firewalld + the sshd port

**The stock `ssh` service is enabled in exactly one place**:
`playbooks/imports/play-lxc-install-config.yml:128-133`

```
- name: Ensure SSH stays permitted in the default zone (no remote self-lockout)
  ansible.posix.firewalld:
    service: ssh
    state: enabled
    permanent: true
    immediate: true
```

preceded by `firewalld` package install (`:54-55`, incl. `python3-firewall`), `systemd` start (`:100-104`)
and a `firewall-cmd --state` readiness wait (`:111-118`).

**A non-stock sshd port is ALREADY handled**, immediately after, `:134-176`:

- `stat /usr/sbin/sshd` → `lxc_sshd_binary` (`:161-165`)
- `python3 -m helpers.sshd_ports.cli --sshd-path /usr/sbin/sshd`, `chdir: {{ root_dir }}`,
  `changed_when: false` (`:157-169`)
- loop `ansible.posix.firewalld: port: "{{ item }}/tcp"` over `lxc_sshd_ports.stdout_lines` (`:171-177`)

**There is NO sshd port VARIABLE anywhere** in `vars/` or `environment/`. The port is *discovered*, not
declared — deliberately. `helpers/sshd_ports/core.py` parses `sshd -T` and handles both `Port N` and
`ListenAddress host:port` (the latter still prints the default `port 22`, so a naive `^port ` grep opens
the wrong port), plus bare IPv6 addresses that merely look like they end in a port. Tests:
`tests/helpers/sshd_ports/test_core.py`, `test_cli.py`.

Other firewalld users for reference: `playbooks/imports/play-vpn.yml:32` (module),
`play-unifi-controller.yml:129-142` and `play-nordvpn-openvpn.yml:175-200` (raw `firewall-cmd`; the
latter uses a documented `# FAIL-FAST-OK` probe).

⚠ **Cross-reference worth flagging**: `files/usr/local/bin/ssh-suspend-guard:36` hardcodes port 22
(`ss -tnp state established 'sport = :22'`) — the exact assumption `helpers/sshd_ports` exists to
disprove. Not part of issue #44, but it is the same class of bug in the same area.

---

## 8. Docs

**`docs/tmux-sessions.md`** — yes, it has a "What survives what" table at `:27-34`. Current rows verbatim:

| Event                          | Session       |
| ------------------------------ | ------------- |
| SSH connection drops or closes | keeps running |
| Laptop sleeps, network changes | keeps running |
| You detach (F12, Detach)       | keeps running |
| Host reboots                   | gone          |

Followed immediately by (`:36`):

> Sessions are transient dev state by design. Nothing restarts them after a reboot.

**Both the "Host reboots | gone" row and that sentence are direct contradictions of issue #44 and must
change with the code** (CLAUDE.md docs-drift gates will catch it at commit time).

Other headings in that file: "The three things you need" (`:7`, incl. the F12 key table at `:16-25`),
"CCY sessions have their own server" (`:38`), "How you know you are inside one" (`:47`),
"Why the tmux status bar is off" (`:53`), "Anything else" (`:59`).

**`docs/ccy.md`** — exists, ~1175 lines. Main sections: Quick Start (`:33`), Installation (`:62`),
What Happens When You Run `ccy` (`:100`), **Sessions Survive the Terminal (`:149`)**, The Security Model
(`:205`), State and the `.claude/ccy/` Directory (`:286`), Tokens (`:370`), **Command Reference (`:488`)**
with sub-tables Session / Image and updates / Auth, SSH and network / Engine and policy / Claude Code
environment CCY sets, Per-Project Configuration (`:635`), The Supervisor (`:766`), Networking (`:882`),
Container Labels (`:901`), SSH and GitHub (`:933`), Extra Mounts (`:992`), Keeping CCY Current (`:1061`),
Troubleshooting (`:1113`, a table — row `:1130` covers "Terminal died; where is my session?"),
Working Inside a CCY Container (`:1138`), See Also (`:1172`).

Places that would need edits for #44: the `### Session` flag table (`:494-506`, which is where
`ccy-sessions` is documented as "Separate command: list every CCY tmux session, attach or end one"),
the Troubleshooting table (`:1113`), and §"Sessions Survive the Terminal" (`:149-169`).

Changelog for CCY version bumps: `docs/ccy-changelog.md`.

---

## Prior art in the plan tree

Completed plans that own this machinery — read before designing:

- `CLAUDE/Plan/Completed/00111-terminal-death-takes-all-ccy-sessions/` — built `lib/tmux-session.bash`,
  `ccy-sessions`, the dedicated socket and the one-terminal rule (CCY 3.52.0).
- `CLAUDE/Plan/Completed/00105-tmux-sessions-single-key-menu/` — `play-tmux-sessions.yml`,
  `/etc/tmux.conf`, `docs/tmux-sessions.md`.
- `CLAUDE/Plan/Cancelled/00036-cc-ccy-parity/`, `CLAUDE/Plan/Completed/00048-…` (cc token parity) —
  context for the shared `cc`/`ccy` prefix design.

Next plan number (from the daemon counter, not a folder scan): **00135**.

## Existing reboot-adjacent machinery

- `files/usr/local/bin/shutdown-with-update` (119 lines) + `files/home/bashrc-includes/shutdown-with-update.bash`
  (an alias to `sudo /usr/local/bin/shutdown-with-update`). A user-invoked pre-shutdown updater. It is
  **not** a hook on `systemctl reboot` — nothing intercepts a plain reboot today.
- `files/usr/local/bin/ssh-suspend-guard` + `playbooks/imports/play-prevent-ssh-suspend.yml` — the
  established pattern for a guard that holds a `systemd-inhibit --what=sleep` lock in a loop, deployed
  as a **system** unit (`/etc/systemd/system/ssh-suspend-guard.service`). The nearest existing analogue
  to "warn before reboot" (`--what=shutdown` is the sibling inhibitor).
- `files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend` — precedent for a systemd lifecycle
  hook script shipped by this repo.
