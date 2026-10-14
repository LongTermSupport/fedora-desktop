# ccy-sessions: functional specification for the Go port

Source of truth: `files/home/.local/bin/ccy-sessions` (1228 lines, last changed at commit 7a8e264d).
`L<n>` means a line of that file unless another file is named. Libraries it sources from
`files/var/local/claude-yolo/lib/`: `common-pure.bash` (CP), `session-registry.bash` (SR),
`tmux-session.bash` (TS). Suites that drive it: `scripts/test-ccy-sessions-reboot.bash` (R),
`scripts/test-ccy-sessions-take-over.bash` (T), `scripts/test-ccy-tmux-targets.bash` (G),
`scripts/test-ccy-restore-askpass.bash` (A). Suites that only source the libraries and so are the
golden cases for the Go ports of the library functions: `scripts/test-ccy-session-registry.bash`
(RG), `scripts/test-ccy-session-network.bash` (N). Structure follows `SPEC-ccy-teams.md`.

stdout carries exactly three things: the `--help` text (L158), the `--list` table (L1065-1066), and
the `verify-restore` report lines (L996). Everything else (status, progress, errors, the
countdown, `would ...` lines) goes to **stderr**. Keep it that way (CLAUDE/StderrHygiene.md). The
picker's own screen is fzf's, drawn on the tty.

---

## 0. Deployment

- **Copied, not templated.** `playbooks/imports/play-claude-yolo.yml` L832-841 (task "Deploy the
  ccy-sessions Command", `ansible.builtin.copy`, src `{{ root_dir }}/files/home/.local/bin/ccy-sessions`,
  dest `/home/{{ user_login }}/.local/bin/ccy-sessions`, mode 0755, tags `scripts`, `launchers`).
  The file has no Jinja token and no `{{ root_dir }}` use: unlike ccy-teams the port needs **no
  ROOT_DIR injection** (no checkout lookup anywhere). The comment above the task (L829-831,
  "attaches a detached one by number") is stale and should be corrected when the task changes.
- The same play deploys the units (all user-scope, `/home/{{ user_login }}/.config/systemd/user/`):
  - `ccy-sessions-restore.service`: `copy` (L880-888), source
    `files/home/.config/systemd/user/ccy-sessions-restore.service`. `Type=oneshot`,
    `ExecStart=%h/.local/bin/ccy-sessions restore`, `Wants=ccy-sessions-set-going.service`,
    `WantedBy=default.target`. Enabled only when `ccy_restore_sessions` is true (L939 enable task;
    L950 removes the `default.target.wants/` symlink otherwise).
  - `ccy-sessions-set-going.service.j2`: `template` (L901-908). One Jinja variable,
    `{{ ccy_restore_compact_floor }}` (and `ansible_managed`), written as
    `Environment=CCY_RESTORE_COMPACT_FLOOR_TOKENS=<n>`. `Type=exec`,
    `ExecStart=%h/.local/bin/ccy-sessions set-going`, `After=ccy-sessions-restore.service` (no
    `Requires=`, deliberately), no `[Install]`.
  - `ccy-sessions-restore.service.d/ssh-unlock.conf`: `copy` (L1004-1010), server profile only
    (removed elsewhere, L1015): `Environment=CCY_RESTORE_SSH_PASSPHRASE_FILE=%h/.claude-tokens/ccy/restore-ssh-passphrase`.
    TS/SR never read the unit; `ccy-sessions restore` reads the env var (SR L1430).
  - The play's read-back checks (L1050-1100) name `ccy-sessions-restore.service` and
    `ccy-sessions-set-going.service`; they are independent of the binary.
- **Paths the Go binary must keep**: `~/.local/bin/ccy-sessions` (units, `shutdown-with-update`
  L235, `helpers/self_update/cycle.py` L822 all call that exact path), mode 0755. Environment the
  units give it: `HOME`, `PATH` (systemd's), `CCY_RESTORE_COMPACT_FLOOR_TOKENS`,
  `CCY_RESTORE_SSH_PASSPHRASE_FILE`. No login-shell variables (`XDG_STATE_HOME`, `CLAUDE_CONFIG_DIR`,
  `TMUX`) can be assumed.
- **Callers (the contract a Go binary must keep)**:
  - `files/usr/local/bin/shutdown-with-update` L111, L235-237: runs
    `timeout --kill-after=10 N sudo -u USER -H ~USER/.local/bin/ccy-sessions notify going-down --minutes M [--dry-run]`. Needs: exit 0/non-zero; a SIGTERM from `timeout` must end it.
  - `helpers/self_update/cycle.py` L822-829: `ccy-sessions notify <args>` and
    `ccy-sessions verify-restore --wait <secs>`; exit 0 only if every restored session is OK.
  - `files/home/.local/bin/ccy-teams.j2` L1652 and L1851 only mention the name and the unit's wants
    symlink; `files/var/local/claude-code/cc` L59 and `claude-yolo` L1196-1206 mention
    `ccy-sessions restore` setting `CCY_SESSION_RESTORE=1`.
- **The bash libraries stay** until phase 3 ports `ccy` itself: `claude-yolo` and `cc` source TS/SR
  (`ccy_tmux_insulate`, `ccy_registry_write`, the trampoline). The Go and bash sides must agree on
  the record format, the manifest format, the tmux session shape and the trampoline text.
  `ccy-sessions` today **requires** TS to exist (L169-176); see 1.2.

## 1. Commands, flags, usage, exit codes

### 1.1 Usage text (stdout, exit 0)

`usage()` (L140-142) prints the file's own header comment: awk from line 2 to the first line that
does not start with `#`, each line with its first two characters (`# `) removed; a bare `#` line
prints empty. That is **L2-L109** (108 lines, last line `Runs on the HOST: inside a CCY container there is no server.`). The Go binary must embed that text verbatim (R L290 asserts it contains
`notify`, `reboot` and `restore`). The usage block proper (L37-71):

```
Usage:
  ccy-sessions                                    the picker (needs a terminal)
  ccy-sessions -l|--list                          print the same table, with a heading, on
                                                  stdout and exit; no terminal needed
  ccy-sessions notify going-down --minutes N [--dry-run]
                                                  tell every live session's project that
                                                  the machine goes down in N minutes, as
                                                  the kind below that is TRUE here
  ccy-sessions notify reboot-warning --minutes N  "reboot; a session restore will follow"
  ccy-sessions notify shutdown-warning --minutes N  "shut down; NO restore, leave a handoff"
  ccy-sessions notify reboot-cancelled            tell them it is not happening after all
  ccy-sessions reboot --in N [--dry-run]          warn, count down, warn again at one
                                                  minute, then systemctl reboot; refused
                                                  up front unless logind lets this account
                                                  reboot with nobody to authenticate
  ccy-sessions restore [--dry-run]                start every session recorded as running
                                                  at the last shutdown (the boot unit)
  ccy-sessions set-going                          once each session this boot's restore
                                                  started has drawn Claude's prompt, type
                                                  its first input: /compact when its
                                                  context is at or above
                                                  CCY_RESTORE_COMPACT_FLOOR_TOKENS, else
                                                  continue (the unit after the restore)
  ccy-sessions verify-restore [--wait SECONDS]    check every session this boot's restore
                                                  brought up, one line each on stdout:
                                                  "<name> OK", "<name> STARTING",
                                                  "<name> SETTING-GOING",
                                                  "<name> NOT-SET-GOING <why>",
                                                  "<name> WAITING-AT-PROMPT <which>" or
                                                  "<name> DEAD <why>"; exit 0 only if all
                                                  are OK. --wait polls until the restore
                                                  has settled (nothing STARTING or
                                                  SETTING-GOING, and the same answer twice
                                                  running), or the time is up
  ccy-sessions -h|--help                          this text
```

The header also documents (L2-35, L73-109): row columns and the words `none`, `no container`,
`unknown`; token/key columns; the CPU column ("about a second to appear"); the keys (Enter, Ctrl-T,
Ctrl-X, Ctrl-N, Esc/q); that notify/reboot use each project's own hooks-daemon CLI
(`signal <kind> --minutes N`); `Exit 64 for a usage mistake, 1 for a refusal.` (L78; it omits 130,
see 1.3); the going-down kind rule; what a restore answers; what set-going does and its env knobs
(`CCY_SESSIONS_SET_GOING_WAIT`, `CCY_SESSIONS_START_WINDOW`).

### 1.2 Dispatch (L149-176, L1003-1024)

| argv[1]                                                          | action                                                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| (absent, or the empty string)                                    | picker (an empty `$1` is treated as absent; further args are then ignored, L151) |
| `-l` / `--list`                                                  | list; any further arg: usage error `'<arg1>' takes no further arguments.` (L153) |
| `-h` / `--help`                                                  | usage to stdout, exit 0, **before** the CCY_LIB guard                            |
| `notify` / `reboot` / `restore` / `set-going` / `verify-restore` | the subcommand with the remaining args (shift at L165-167)                       |
| anything else                                                    | usage error `unknown option '<arg1>'.`                                           |

`usage_error <msg>` (L144-147): stderr `ccy-sessions: <msg> See ccy-sessions --help.`, exit 64.
Subcommands run **headless** (no terminal needed, L1071-1073); only the picker needs one.

Guard (L169-176), for every mode except `--help`: if `${CCY_LIB:-/var/local/claude-yolo/lib}/tmux-session.bash`
is not a regular file: if `/workspace` is a directory print
`ccy-sessions runs on the host, not inside a CCY container.` else
`CCY is not installed here (<CCY_LIB>/tmux-session.bash missing). Run playbooks/imports/play-claude-yolo.yml.`;
exit 1. R sets `CCY_LIB` to the repo's lib directory on every call (R L281). The Go
`internal/teams` already does the same file test (`teams/app.go` L73-84): reuse that code (move it
to a shared place; see section 8). Decide whether to keep the file test once ccy is ported (it is
only a proxy for "CCY is installed").

### 1.3 Exit codes (actual)

| code     | when                                                                                                                                                                                                        |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0        | success; `--help`; picker left by Esc/q/Exit (L1189); picker attached then the user detached (L1101); no sessions; `reboot --dry-run`; `notify` all delivered; `reboot` after `systemctl reboot` returned 0 |
| 1        | any refusal or failure (every `return 1` reaches `exit` through `set -e`); the picker without a terminal (R L292); three picker tries used (L1226); list unreadable (L1176); take-over failure              |
| 64       | `usage_error` (R L295, L354, L356, L358, L360, L391, L439, L441, L889, L999)                                                                                                                                |
| 130      | `reboot` interrupted by INT or TERM during warning or countdown (L502; R L640)                                                                                                                              |
| rc       | `reboot` when `systemctl reboot` exits rc != 0: returns **rc itself** (L583), e.g. 1 (R L534)                                                                                                               |
| launcher | Ctrl-N / "start ccy here": `exec` of the launcher, so the process exit status is the launcher's                                                                                                             |
| leaked   | an unguarded failing command under `set -e`: `confirm_end`'s `kill-session` (L1085) exits with tmux's status (see 9.5); nothing else in this file leaks a child's status                                    |

Actual `exit`/`return` sites and their codes are listed with each flow below.

### 1.4 Constants and environment variables (all read by the tool itself)

| variable                                    | default                              | meaning / where                                                                                |
| ------------------------------------------- | ------------------------------------ | ---------------------------------------------------------------------------------------------- |
| `CCY_LIB`                                   | `/var/local/claude-yolo/lib`         | guard only (L112)                                                                              |
| `CCY_LAUNCHER` (constant, not env)          | `/var/local/claude-yolo/claude-yolo` | what Ctrl-N execs (L113, L1160). Not overridable: untested path                                |
| `MAX_TRIES`                                 | 3                                    | picker tries (L114)                                                                            |
| `CCY_SESSIONS_MINUTE_SECONDS`               | 60                                   | one "minute" of countdown (L117); R sets 0 to run instantly, 60 for the INT/TERM case (R L620) |
| `CCY_SESSIONS_POLL_SECONDS`                 | 5                                    | verify-restore --wait and set-going poll interval (L120); R sets 0                             |
| `CCY_SESSIONS_SET_GOING_WAIT`               | 1200                                 | set-going lock wait **and** prompt-wait deadline, seconds (L124); R sets 0 or 5                |
| `CCY_SESSIONS_START_WINDOW`                 | 120                                  | seconds a typed input has to be taken (L127); R sets 0 and 3600                                |
| `CCY_SESSIONS_SUBMIT_DELAY`                 | 0.2                                  | pause between typed text and Enter, **fractional seconds** (L130); R sets 0                    |
| `CCY_RESTORE_COMPACT_FLOOR_TOKENS`          | (none)                               | set-going floor (L810); must match `^[1-9][0-9]*$`                                             |
| `CCY_RESTORE_SSH_PASSPHRASE_FILE`           | (none)                               | restore only (SR L1430); see 4.7                                                               |
| `CCY_STATE_DIR` / `XDG_STATE_HOME` / `HOME` | see 6.1                              | registry and manifest location (SR L54-71); A and R set `CCY_STATE_DIR`                        |
| `CCY_BOOT_ID_FILE`                          | `/proc/sys/kernel/random/boot_id`    | boot id source (SR L716); R sets it (R L908)                                                   |
| `CLAUDE_CONFIG_DIR`                         | `$HOME/.claude`                      | cc transcript home (L719); R unsets it (R L1021)                                               |
| `CCY_CPU_SAMPLE_SECONDS`                    | 1                                    | CPU sample length, fractional allowed (TS L532); R and T set 0                                 |
| `CCY_TMUX_TAKE_OVER_WAIT_TENTHS`            | 50                                   | take-over wait, tenths of a second (TS L151); T exports 3 (T L230) and 20                      |
| `NO_COLOR`, `TMUX`, `PWD`, `HOME`           |                                      | `PWD`/`HOME` build the `~` form of the directory (L1153, TS L770)                              |

Constants of the tool (L134-135): `SET_GOING_COMPACT` is
`/compact This session was restored after a reboot. After compacting, carry on with the work that was in progress.`
and `SET_GOING_CONTINUE` is `continue`. `MAX_TRIES`, the tmux socket `ccy` (TS L32; **not** overridable
by the binary, only by the test that rewrites the variable after sourcing, G L71) and
`CCY_CONTAINER_WORKSPACE=/workspace` (L199).

## 2. The table: `--list` and the picker's rows

### 2.1 `--list` (L1059-1068)

```
listing="$(rows)" || exit 1          # rows failure: exit 1 (message already printed)
empty  -> stderr "No CCY or cc sessions are running."; exit 0; stdout EMPTY (R L332)
else   -> stdout: heading line, then the rows, one per line; exit 0
```

Output is `ccy_tmux_row_heading` (no trailing newline) then `printf '\n%s\n' "$listing"`: the heading is
immediately followed by the rows (no blank line). `R L317` counts 3 lines for 2 sessions. Both `-l`
and `--list` (R L312). No colour anywhere in the table.

### 2.2 Row layout (TS L749-778)

Full row: `printf '%-28s  %-15s  %5s  %-22s  %-16s  %-16s  %s'` over
`NAME STATE CPU NETWORK TOKEN KEYS DIRECTORY`; heading is the same format over
`SESSION STATE CPU NETWORK TOKEN "SSH KEY" DIRECTORY` (TS L754-756). R L319 asserts the heading
contains `CPU`, `TOKEN`, `SSH KEY` in that order.

- STATE is `detached` when the `#{session_attached}` count is `0`, else `open elsewhere` (TS L765-768).
  The picker tests the plain row text for the substring `open elsewhere` (L1212); see 9.9.
- Directory is the tmux `session_path` with a leading `$HOME` replaced by `~` using bash `${3/#${HOME}/\~}`:
  a **prefix** replace with no `/` boundary (see 9.10). `term.AbbrevHome` (term.go L115-126) requires
  the boundary and is the correct behaviour.
- The last column is not padded. Width counts characters (data is ASCII in practice).
- Shorter forms (network only, or none) exist for ccy's re-attach offer (TS L773-777, L326) and are
  **not** used by ccy-sessions: it always passes all four extra columns, and a failed probe passes
  the literal `unknown`.

### 2.3 `rows` (L1034-1057)

1. `details="$(ccy_tmux_detail_rows)"`. On success each line `<session>\t<network>\t<token>\t<keys>\t<cpu>`
   (tabs; read with `IFS=$'\t'`) fills four per-name maps. On failure (the reason is already on
   stderr) print `The CPU, network, token and SSH key columns read 'unknown' for every session — see the error above.`
   and carry on.
2. `listing="$(ccy_tmux_list)" || return 1` (read whole first so a failure is not "no sessions").
3. For each `name attached dir` (`read -r`: the directory is the rest of the line and may hold spaces;
   R uses a directory with a space): `ccy_tmux_row name attached dir <network|unknown> <token|unknown> <keys|unknown> <cpu|unknown>`.
   A session that is in `list-sessions` but missing from the probe (no pane) reads `unknown` in all
   four columns (this is what T's picker sees, since its fake `list-panes` prints nothing).

### 2.4 The probe `ccy_tmux_detail_rows` (TS L648-743), exact calls and order

Runs in an `if` context (errexit off inside), so only the explicit `return 1`s fail it.

01. `tmux -L ccy list-panes -a -F '#{session_name} #{pane_pid}'` (stderr merged). Failure whose text
    contains `no server running` or `No such file or directory` -> **return 0 with no output** (no
    sessions, and no sleep). Any other failure: `ERROR: tmux list-panes failed: <text>`, return 1.
02. `ps -ww -eo pid=,ppid=,args=` (stderr merged). Failure: `ERROR: the process table could not be read, so no session can be matched to a container: <text>`, return 1. **Go must run `ps`
    (R's fake `ps` is on PATH, R L120-123) and not read /proc for the table.**
03. `ccy_session_containers(panes, ps)` (pure, TS L380-451; golden cases N L58-132). Per ps line
    (`read -r pid ppid args`): record `parent[pid]=ppid`; engine = basename of first word of `args`;
    only `podman`/`docker`. Container = the value after the first `--name` or `--name=` found scanning
    fields from index 1 (whitespace split of `args`); none -> skip. Clients in table order. Per pane
    line (`session pid`): `session_of[pid]=session`, session order = first appearance. For each
    client in table order climb parents (cap 32 hops, stop at `""` or `0`) until a pid in
    `session_of`; the **first** client that reaches a session owns it. Output `"<session> <engine> <container>"`
    per session in pane order, `"- -"` when none.
04. Engines used = set of engines with a container. For each engine (bash map order; **sort in Go**):
    `<engine> ps --filter label=ccy=true --format '{{.Names}}|{{.Networks}}|{{.Label "ccy-token"}}|{{.Label "ccy-ssh-keys"}}'`
    (stderr merged; failure -> `ERROR: <engine> ps failed: <text>`, return 1). Lines split at `|`
    into `name|nets|token|keys` (keys takes the rest of the line). Builds a networks table
    (`"<name> <nets>"`), a labels table (`"<name>|<token>|<keys>"`) and the running set.
    The argv is asserted by the podman fake with `case "$*"` (R L146); N pins both formats (N L283-289, L330).
05. For each engine that holds a **running** container of a session (`running[container]`), one
    `<engine> inspect --format '{{.Name}} {{.State.Pid}}' <container>...` (names in session order,
    one argv each; stderr merged; failure -> `ERROR: <engine> inspect failed: <text>`, return 1).
    Output `"<name> <pid>"`; a leading `/` on the name (Docker) is stripped. R's fake answers exactly
    `inspect --format {{.Name}} {{.State.Pid}} a_yolo` (R L150).
06. Roots = the pane lines plus `"<session> <container pid>"` for each container with a pid other
    than `0`. `ccy_session_pids(roots, ps)` (pure, TS L538-575): for each ps pid climb (cap 64) to a
    root; append the pid to that root's session. Output `"<session> <pid>..."`.
07. `getconf CLK_TCK` (failure -> `ERROR: the clock tick rate could not be read, so no CPU share can be worked out: <text>`, return 1). **Go can use `sysconf(_SC_CLK_TCK)`/100; there is no fake for getconf,
    so either is fine for the suites.**
08. `before = ccy_cpu_sample(all session pids)`; `sleep $CCY_CPU_SAMPLE_SECONDS`; `after = ...`. A
    sample is the clock in microseconds, then `"<pid> <utime+stime>"` from `/proc/<pid>/stat` (fields
    14 and 15, counted after the last `) `); an unreadable pid is skipped silently (it exited).
09. `ccy_cpu_words` (pure, TS L584-611; N L191-247): `denominator = elapsed_us * hz`; `<= 0` -> `ERROR: a CPU sample needs time to pass and a tick rate; got <us> us at <hz> ticks a second`, return 1.
    Per session: sum over its pids present in `after` of `now - was` (`was` defaults 0 for a process born
    during the sample; a pid missing from `after` contributes nothing). Percent =
    `(used*100000000 + denominator/2) / denominator` (integer, half-up), printed `N%`.
10. Output, one line per **session in pane order**: `session \t network_word \t token \t keys \t cpu`
    where `network_word` is `ccy_network_word(container, networks)` (TS L466-492): `no container` for
    container `-` or a container not in the engine's list; `[a b]` brackets stripped, commas -> spaces,
    split, join with `,`; empty -> `none`. `ccy_identity_words` (TS L503-520): `-\t-` for no
    container; else `<token or "unlabelled">\t<keys joined with "," or "unlabelled">` (a labelled
    container whose label is empty reads `unlabelled`; the launcher writes `none`).

If the container map is empty the probe still takes both samples and sleeps (when the server exists).

## 3. The picker (L1074-1228)

### 3.1 Start-up

- **Terminal gate** (L1074-1077): stdin **and** stdout must be terminals, else stderr
  `ccy-sessions needs a terminal: it attaches sessions to the one you are in. ccy-sessions --list prints the table instead.`, exit 1 (R L292-293). Order: after the subcommand dispatch and `--list`.

- `here = $PWD` with a leading `$HOME` replaced by `~` (L1153; the **environment variable** PWD, not `getcwd`).

- `project_here` = `git rev-parse --is-inside-work-tree 2>&1` succeeds **and** prints exactly `true`
  (L1154-1157); run in the process cwd. `gitx.ProjectHere(ctx, "")` is the same (git refusing is
  false; git unrunnable is an error: in bash a missing `git` is also just false).

- `new_line`: `Ctrl-N starts a new ccy session in <here>` or
  `Ctrl-N unavailable: <here> is not a project folder (no git repository)` (L1164-1166). Computed once.

- `HEADER` (L1168-1172) = `ccy_tmux_header` of, in order:

  1. `Enter attach   Ctrl-T take over   Ctrl-X end   Ctrl-N new   Esc or q quit` (the keys line; prefixed `↑↓ choose   `)
  2. `new_line`
  3. `"open elsewhere" is attached in another terminal: Ctrl-T detaches that terminal and attaches this one; the session keeps running` (the quotes are literal)
  4. `CPU: since the list last redrew ("-" until it has), 100% is one core` (the bash original sampled one second per redraw; Plan 00192 measures against the previous redraw instead)
  5. `network, token, SSH key: what its container is on and was started with — "no container" and "-" mean nothing to ask`

  `ui.Header(keys, lines...)` (picker.go L114-121) is the same builder (empty lines dropped, joined by `\n`).

### 3.2 The loop

```
attempt = 1
loop:
  listing = rows()  || exit 1
  if listing empty:
      if project_here:
          if confirm("CCY sessions", "No sessions are running. Start ccy in <here>?", "start ccy here")  -> start_ccy_here (exec; never returns)
          exit 0
      stderr "No CCY or cc sessions are running. Start one with ccy or cc in a project directory."; exit 0
  picked = pick("CCY sessions", HEADER, expect "ctrl-t,ctrl-x,ctrl-n", rows)   # non-zero -> exit 0  (Esc, q, Exit row, ANY fzf failure)
  key = line 1, row = line 2, name = first word of row
  case key:
    ctrl-n : if project_here -> start_ccy_here ; else stderr "<here> is not a project folder (no git repository), so ccy cannot be started here."   -> counts a try
    ctrl-x : confirm_end(name); continue                  # no try used, whatever the answer
    ctrl-t : if confirm_take_over(name) (returns 0 = declined) -> continue      # else counts a try
    ""(Enter): if row does not contain "open elsewhere" -> attach_here(name)
               else stderr "'<name>' is open in another terminal. Ctrl-T takes it over here; Ctrl-X ends it."   -> counts a try
  attempt += 1; if attempt > 3: final message; exit 1
```

Final messages (L1220-1227), stderr then exit 1: if `DETACHED_FROM` is non-empty
`Nothing attached after 3 tries. A take-over was tried on <names joined by single spaces>; what each one did is above.`
else `Nothing attached after 3 tries. Nothing changed.` (T L414-423). A take-over that attached
exits 0 at once; a *declined* take-over, a Ctrl-X and a refreshed list after a Ctrl-X do not use a try.

The picker is the shared house fzf (`ui.Picker.Pick`; section 8). The call is
`ccy_tmux_pick(title, header, "ctrl-t,ctrl-x,ctrl-n")` with rows on stdin and the **Exit** row appended;
`--expect=ctrl-t,ctrl-x,ctrl-n` is passed (T L365 asserts `ctrl-t` is in fzf's argv), output is
`<key>\n<row>`; the Exit row returns non-zero.

### 3.3 `start_ccy_here` (L1158-1161)

stderr `Starting ccy in <here>.` then `exec /var/local/claude-yolo/claude-yolo` with no arguments, cwd and environment unchanged (the launcher then does its own checks and prompts). Go: `syscall.Exec`; needs an
`execx`-level seam for tests (none exists; no suite covers it).

### 3.4 `attach_here` (L1096-1105)

`ccy_tmux_attach name`: rc 0 -> `exit 0` (attached then detached, or session ended while attached);
rc 3 (this terminal was kicked by the hook; the library already printed
`ERROR: '<name>' is open in another terminal, so this terminal was not attached to it.`) -> return, a try;
**any other rc (1 or 2)** -> `'<name>' was taken or ended just now — the list has been refreshed.`, return, a try.
`ccy_tmux_attach` (TS L115-140), in order: `is_detached` false (open, gone, or **a tmux failure**) ->
`ERROR: '<name>' is open in another terminal, or gone. It can be attached from one terminal only.`, return 2;
`tmux -L ccy set-hook -g client-attached '<hook>'` (failure -> return 1, tmux's own stderr);
stderr `Attaching to '<name>'.`; `tmux -L ccy attach-session -t =<name>` on the terminal (failure:
`ERROR: could not attach to '<name>'`, return 1); then `list-clients -t =<name> -F '#{client_tty} #{client_activity}'`:
session gone -> `'<name>' has ended.` return 0; clients present -> the rc 3 error above; none ->
`Detached. '<name>' keeps running; run ccy in its project directory to return to it.` return 0.
Hook text (TS L105-108) exactly: `if-shell 'test $(tmux -L ccy list-clients -t "#{session_name}" | wc -l) -gt 1' 'detach-client'`
(`tmuxccy.SingleAttachHook()` is identical).

### 3.5 `confirm_take_over` (L1118-1148) and `ccy_tmux_take_over` (TS L213-256)

```
others = other_terminals(name)         # list-clients; words "<tty> (idle …), <tty> (idle …)"
  rc 2 (session gone)      -> stderr "'<name>' has ended — the list has been refreshed."; return 1 (a try)
  rc other non-zero        -> exit 1   (library printed the error)
others empty              -> attach_here(name); return 1 (a try if it returns)
confirm("Take over <name>?", "It is open on <others>. That terminal is detached, not closed, and the session keeps running.", "take over '<name>'")  (cursor on Exit)
  declined -> stderr "Left '<name>' where it is."; return 0 (NO try; the list is redrawn)
take_over(name):  rc 0 -> exit 0 ; rc 2 -> DETACHED_FROM += name; stderr "The list has been refreshed."; return 1 (a try) ; any other rc (1, 3) -> exit 1
```

`ccy_tmux_take_over(name)`: `list-clients` (gone -> `ERROR: '<name>' has ended; there is nothing to take over.` return 2; error -> return 1).
If clients: `words = client_words(clients, now)`; `tmux -L ccy detach-client -s =<name>` (stderr merged);
success -> stderr `Detached '<name>' from <words>; the session keeps running.`; failure -> `detach_failed`,
stderr `The detach from <words> failed (<out>); checking whether '<name>' is free anyway.`. Then loop:
`list-clients`; gone -> `ERROR: '<name>' ended while it was being taken over; nothing was attached.` return 2;
error -> return 1; empty -> break; non-empty and `detach_failed` -> `ERROR: could not detach '<name>' from <words>: <out>` return 1;
`waited >= WAIT_TENTHS` -> `ERROR: '<name>' was told to leave <words>, but it is still open there after <WAIT_TENTHS/10> s. Nothing was attached; the session keeps running.` return 1;
else `sleep 0.1; waited++`. Finally `ccy_tmux_attach name` (its rc, including 3, is returned). Never kills a session
(T L267, L376 assert no `kill` in the tmux log). `idle_words(s)` (TS L155-169): negative -> 0; `<60` `idle N s`;
`<3600` `idle N min`; `<86400` `idle N h`; else `idle N d` (integer division). `client_words`: `<tty> (<idle words of now-activity>)` joined by `, `.
`other_terminals` uses `now = EPOCHSECONDS`. Golden cases: T L239-257, L259-348.

### 3.6 `confirm_end` (L1082-1091)

`ccy_tmux_confirm "End <name>?" "Everything running in it will be killed." "end '<name>'"` (cursor on Exit). Yes ->
`tmux -L ccy kill-session -t =<name>`, `ccy_registry_remove <name>`, stderr `Ended '<name>'.`; no -> stderr `Kept '<name>'.`.
Both go straight back to the list. `kill-session` is unguarded (9.5).

### 3.7 `ccy_tmux_confirm` / `ccy_tmux_pick` (TS L784-844)

`confirm(title, question, action [, yes])`: fzf with header = `ccy_tmux_header("Enter choose   Esc or q exit", question)`,
rows `Yes, <action>` + `Exit`, cursor on Exit unless the 4th arg is `yes` (`--bind=start:last`),
no `--expect`. Yes only when the chosen row starts with `Yes, `. Esc/q/Exit/any fzf failure = no.
`ui.Picker.Confirm` (picker.go L98-110) implements this. fzf argv (TS L832-836, `ui/picker.go` L49-62): `--ansi --height=~70% --layout=reverse --no-multi --no-sort --no-info --color=<pickerColours> --border=rounded --border-label=" <title> " --border-label-pos=3 --margin=1,2 --padding=1,2 --header-first --pointer=▶ --prompt="filter > " --header="<header>\n" --bind=q:abort [--expect=<keys>] [--bind=start:last]`; the Go order matches the bash order except that bash puts `--expect`/`--bind=start:last` last, as does Go.
Missing fzf: bash prints `ERROR: fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it.` and returns 1,
which every caller treats as "Esc" (so the picker **exits 0**): see 9.1.

## 4. Subcommands

### 4.1 Shared: sessions, containers, routes (L203-243)

`session_containers()` (L203-217): `tmux -L ccy list-panes -a -F '#{session_name} #{pane_pid}'` (no server
text -> empty panes; other failure -> `ERROR: tmux list-panes failed: <text>`, return 1), `ps -ww -eo pid=,ppid=,args=`
(failure -> `ERROR: the process table could not be read, so no session can be matched to its container: <text>`, return 1;
**note the wording differs from the detail probe's**), then `ccy_session_containers` (2.4 step 3).
`set_route(dir)`: if the project has a container route: `ROUTE_CMD = [engine, exec, container, /workspace/.claude/hooks-daemon/bin/hooks-daemon]`, root `/workspace`, via text ` (in container <container>)`;
else `ROUTE_CMD = [<dir>/.claude/hooks-daemon/bin/hooks-daemon]`, root `<dir>`, via empty. No `-i`/`-t`/`-w`/env on `exec` (R L130-142 fake accepts `exec <container> <cli> <args...>`).

### 4.2 `check_projects <kind>` (L259-349)

Fills `PROJECTS` (distinct session directories in first-appearance order) and the routes. Steps:

1. `listing = ccy_tmux_list`; failure -> `ERROR: the live session list could not be read, so nothing is signalled and nothing may reboot.`, return 1 (R L601-604, L606-613).
2. empty listing -> return 0 (no projects).
3. `containers = session_containers`; failure -> `ERROR: the live sessions could not be matched to their containers, so nothing is signalled and nothing may reboot.`, return 1.
4. Per `name attached dir` (skip if name or dir empty): a session named `ccy-*` with no container is **left out**
   (collected as `  <name>  in <dir>`); others join `PROJECTS` (once per dir) with `sessions_of[dir]` accumulating names;
   the project's route is the **first** session in listing order that has a container. If any left out, stderr
   `Left out: no container was found in these ccy sessions' process trees (still at ccy's startup prompts, or the container has exited), so there is no agent in them to warn:` and the lines (R L803 asserts `Left out` and `ccy-s  in <dir>`).
5. `PROJECTS` empty -> return 0.
6. For each project dir: if `<dir>/.claude/hooks-daemon/bin/hooks-daemon` is not executable (`-x`, host path even for a container route) -> `missing`; else
   unless `kind == reboot-cancelled`: `set_route`, run `<ROUTE_CMD> signal --help` (stdout and stderr merged); non-zero -> `broken` with the captured text.
   **This runs on dry runs too** (R L460-462) and is repeated by every `signal_projects` call.
7. No missing and no broken -> return 0. Else, stderr, to a single report:
   - if missing: `ERROR: these live sessions cannot be warned: their project has no hooks-daemon CLI.` then per dir, per session: `  <name>  in <dir>  (expected <dir>/.claude/hooks-daemon/bin/hooks-daemon)`
   - if broken: `ERROR: these live sessions cannot be warned: their project's hooks-daemon CLI cannot run.` then per dir, per session `  <name>  in <dir><via>: its hooks-daemon CLI cannot run:`, then the captured text with every line prefixed by 6 spaces
   - then `For each session above, either end it (run ccy-sessions and press Ctrl-X on it), or install or repair the daemon in its project (the hooks-daemon skill, 'install'). Nothing was signalled.`; return 1.
     R asserts: names and dirs, `no usable venv`, `Ctrl-X`, `'install'`, every session of a missing project listed once (R L581-590), `(in container c_yolo)` (R L493).

### 4.3 `deliver_signal <kind> <minutes|""> <dry-run>` (L356-384) and `signal_projects` (L390-393)

- no projects: stderr `No CCY or cc sessions need warning; nothing to signal.`, prints `0` on stdout, return 0.
- `cli_args = signal <kind> [--minutes N] --all-sessions`; per project in order:
  dry-run -> stderr `would signal <kind>[ --minutes N] to <dir><via>`; else run `<ROUTE_CMD> signal <kind> [--minutes N] --all-sessions --project-root <route root>` (argv order is asserted: R L344, L480);
  failure -> `ERROR: signalling <kind> to <dir><via> failed.`; for a **warning** return 1 at once (no count printed); for `reboot-cancelled` remember and carry on;
  success -> stderr `signalled <kind>[ (N min)] to <dir><via>`.
- after the loop, a remembered failure -> return 1; else print the project count on stdout and return 0.
- `signal_projects` = `check_projects || return 1`, then `deliver_signal`.
- The child's **stdout is not redirected**: under `reboot`'s `reached="$(deliver_signal ...)"` it pollutes the count (9.3).

### 4.4 `notify` (L436-470)

Argument grammar (positional, after `notify`):

| kind                                               | extra arguments                                                                                                                                                                                       |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `going-down`, `reboot-warning`, `shutdown-warning` | `--minutes N` (required, N matches `^[1-9][0-9]*$`) then optional `--dry-run`; a fourth argument that is not `--dry-run` -> `unexpected argument '<arg>'.`; a fifth -> `unexpected argument '<arg>'.` |
| `reboot-cancelled`                                 | none: any extra -> `'notify reboot-cancelled' takes no further arguments.` (`--dry-run` is **not** accepted here)                                                                                     |
| (absent)                                           | `'notify' needs a signal kind: going-down, reboot-warning, shutdown-warning or reboot-cancelled.`                                                                                                     |
| other                                              | `unknown signal kind '<kind>'.`                                                                                                                                                                       |

Errors: `'notify <kind>' needs --minutes N.` (when arg 2 is not `--minutes` or arg 3 empty); `--minutes needs a whole number of minutes, at least 1; got '<v>'.` (all usage errors, exit 64). `going-down` is
rewritten to `reboot-warning` when `${HOME}/.config/systemd/user/default.target.wants/ccy-sessions-restore.service` exists (`-e`: **follows** the link, so a dangling link is "off"; R L382-384), else `shutdown-warning` (L426-434).
Then `signal_projects kind minutes dry-run >/dev/null`: stdout empty, exit status of that call (0/1). A dry run **does** run `check_projects` (R L385-389). Stderr strings asserted: `No CCY or cc sessions` (R L366), `would signal shutdown-warning --minutes 5` (R L389).

### 4.5 `reboot` (L515-585)

Args: `--in N` (`--in needs a number of minutes.` if empty/missing; N validated by the same regex with flag name `--in`), `--dry-run`; anything else -> `unknown reboot option '<arg>'.`; no `--in` -> `'reboot' needs --in N.` (exit 64).
`kind = going_down_kind`.

1. **Up-front checks, all of them, before anyone is told anything**:
   `reboot_permitted || refused=true`, then `check_projects kind || refused=true`. If refused: stderr
   `Nothing was signalled and nothing rebooted.`, exit 1. **`busctl` is the first external command** (R L552).
   `reboot_permitted` (L400-421): `busctl --system call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanReboot` (stdout+stderr merged; the argv is asserted exactly by the fake, R L188).
   Failure -> `ERROR: could not ask logind whether this account may reboot: <text>`, return 1. Reply `s "<word>"`: strip the prefix `s "` and one trailing `"`. `yes` -> ok.
   `challenge` -> `ERROR: this account may reboot only after authenticating to polkit (logind CanReboot: challenge). The reboot comes after the countdown, when nobody may be there to answer, and from a login with no terminal polkit refuses it outright.`;
   anything else -> `ERROR: logind does not let this account reboot (logind CanReboot: <word>), so the reboot would be refused after every session had been warned.`;
   both then stderr `Reboot as root instead, which warns the sessions the same way: sudo /usr/local/bin/reboot-with-update --in N (it applies updates first). Or run ccy-sessions reboot from the machine's own desktop session.` and return 1 (R L553-563 asserts `CanReboot: <word>`, `sudo /usr/local/bin/reboot-with-update`, and that "staying up"/"cancelled" are absent).
2. Not dry-run: `REBOOT_WARNED=true`; install an **exit handler** and an **INT/TERM handler** *before the first warning*.
3. `reached = deliver_signal(kind, N, dry)` (a failure -> `return 1`, which triggers the exit handler). stderr `Rebooting in <N> minute(s); <reached> project(s) warned.`
4. If `N > 1`: dry-run -> stderr `would wait <N-1> minute(s), then warn again at one minute`; else wait `(N-1)*MINUTE_SECONDS` seconds (interruptible), then `signal_projects kind 1 false >/dev/null || return 1` (re-runs `check_projects`, including the `signal --help` probes, and refreshes the project list), stderr `Rebooting in 1 minute.`. For `N == 1` there is no second warning.
5. Dry-run -> stderr `would reboot (systemctl reboot)`, return 0.
6. Wait `MINUTE_SECONDS` (interruptible).
7. **Ignore INT and TERM from here on** (a shutdown reaching the process is not a cancellation; R L776-782). `systemctl reboot` (argv exactly `reboot`, R L415 counts `^systemctl reboot$`). rc != 0 -> `REBOOT_FAILED=true`, `ERROR: the reboot failed: systemctl reboot exited <rc> (its reason is above). The machine is staying up.`, return **rc**.
8. Exit handler (L478-493): on any exit with status != 0 after `REBOOT_WARNED`: stderr `Telling the sessions the warned reboot is not happening.` if `REBOOT_FAILED` else `The machine is staying up; telling the sessions the reboot is off.`; run `signal_projects reboot-cancelled "" false >/dev/null` (continues past a failing project); failure -> `ERROR: the sessions could not all be told; some still expect the reboot.`; then exit with the **original** status. Status 0 withdraws nothing (R L541-542).
9. INT/TERM handler (L495-503): remove the handlers; kill the countdown sleep (`(the countdown had already ended)` if it already had); stderr empty line then `Reboot cancelled.`; exit **130** (then the exit handler withdraws). Output asserted: `Reboot cancelled` and exit 130 (R L640-643); and the reboot-failed text is not `Reboot cancelled` / `reboot is off` (R L539). A signal arriving while a child runs takes effect after the child finishes (bash trap semantics); in Go handle at the next select point and `killpg`/ignore as chosen, the tests only deliver TERM during the 60 s sleep.
   "no sessions" still warns/counts: `reached` is `0` and the reboot proceeds (R L507-511).

Order assertions in R: first warning carries the full minutes for each project, the 1-minute warning, then `systemctl reboot` **last** (R L413-416); project signalling is in listing order (R L515-518 relies on A before B).

### 4.6 `restore` (L587-598; lib `ccy_registry_restore`, SR L1421-1529)

`restore [--dry-run]`; `restore ''` and `restore --dry-run x` etc: usage errors `unknown restore option '<arg>'.` / `unexpected argument '<arg2>'.` (R L888-889). (An empty-string first arg slips through the case and fails in the library with `ERROR: ccy_registry_restore accepts only --dry-run, not ''.` exit 1: oddity, 9.12.)

Behaviour:

1. `passphrase_file = $CCY_RESTORE_SSH_PASSPHRASE_FILE`, then **remove it from the environment** of this process (so tmux's server never inherits it; A L488-489). If set and `ccy_restore_passphrase_check` fails (SR L1352-1375: must be a regular file, owned by the effective uid, mode 600 or 400, readable and non-empty; messages in SR) -> `ERROR: nothing is restored: every ccy session would stop at its SSH key passphrase prompt.`, return 1 *before anything starts* (A L492-496: output names the missing file and `play-claude-yolo.yml`).
2. `regdir` per 6.1. Exists but not a readable+searchable directory -> `ERROR: the session registry <regdir> is not a readable directory, so the sessions recorded there cannot be listed; nothing is restored.`, return 1. Absent -> stderr `ccy session registry: nothing to restore (<regdir> does not exist).`, and (not dry-run) `_ccy_restore_manifest_merge` with no entries (writes a manifest with the boot id and no entries), return 0.
3. `ccy_tmux_list` failure -> `ERROR: the live session list could not be read, so nothing is restored: starting sessions on top of an unknown live set could double every one of them.`, return 1. `live` = names.
4. For each file in the registry directory, **glob order = bash sorted by locale collation** (use byte-sorted names), skipping `*.tmp.*`: `seen = true`; `ccy_registry_read` failure -> failures++ (the reader printed why); `restore == no` -> stderr `skip <name>: marked no-restore.`; compute `args = ccy_registry_restore_args --compose <compose> <prefix> <recorded args>` (failure -> `ERROR: could not work out the arguments to restore <name> with (the record is kept at <file>).`, failures++);
   `resume = resume_id(args)`, `supervised = supervised(prefix, args)`;
   live name -> stderr `skip <name>: already running.` and a manifest entry `(name prefix dir resume supervised keep "" "" "")`, continue;
   `dir` not a directory -> `ERROR: cannot restore <name>: its directory no longer exists: <dir> (the record is kept at <file>; remove it if the project is gone).`, failures++;
   dry-run -> stderr `would start <name> in <dir>: <launcher> <args joined by single spaces>`, continue;
   else `ccy_tmux_start_detached name dir env CCY_SESSION_RESTORE=1 [CCY_RESTORE_SSH_PASSPHRASE_FILE=<path>] <launcher> <args...>` (the passphrase assignment only when a file is set **and** `prefix == ccy`; a `cc` session never gets it: A L484-486). Success -> stderr `restored <name> in <dir>.`, started++, entry `(... pending "" "" "")`; failure -> `ERROR: could not start <name> in <dir> (the record is kept at <file>).`, failures++.
5. Not dry-run: `_ccy_restore_manifest_merge <entries>` under the manifest lock (6.2); failure -> failures++.
6. No files seen -> stderr `ccy session registry: nothing to restore (no records in <regdir>).` and exit status = (failures == 0). Else stderr `ccy session restore: <started> started, <failures> failed.`, status (failures == 0). Returns as soon as the sessions exist (it is a `Type=oneshot` that default.target waits for).

`ccy_registry_restore_args` (SR L629-680; golden cases RG L373-402, L653-805), pure: `--compose start` for outcome `started`/`running`, `--compose skip` for `declined` (ccy only; unknown outcome is an error); `--supervise` appended (before the first `--` if any) for ccy unless the recorded args hold `--supervise`/`--no-supervise` before `--`; `--continue` appended after everything unless the args hold `--continue`, `-c`, `--resume` or `-r`. For prefix `cc`, only `--continue`.
`ccy_registry_resume_id` (SR L980-1011) and `ccy_registry_supervised` (SR L1017-1032): pure, golden RG L926-937. The record's `launcher` is run as given (e.g. `/var/local/claude-yolo/claude-yolo`).

**`ccy_tmux_start_detached` (TS L965-990) — the exact invocation to reproduce**:

```
systemd-run --user --scope --quiet --collect \
    --unit ccy-tmux-<pid of this process>-<systemd-escape -- NAME> --description "CCY tmux session NAME" \
    -- tmux -L ccy new-session -d -s NAME -c DIR -- bash -c <trampoline> ccy-tmux <cmd> [args...] \; \
    set-hook -g client-attached <hook>
```

(`\;` is a single argv element `;`.) Preconditions with their errors: `tmux` on PATH (`ERROR: tmux is not installed, so session '<name>' cannot be started. Deploy it with playbooks/imports/play-tmux-sessions.yml (part of playbook-main.yml).`), then `systemd-run` and `systemd-escape` (`ERROR: <tool> is not available, so session '<name>' cannot be started. It ships with systemd, ...`). The record path in the trampoline is `<regdir>/<name>`.
Trampoline (`ccy_registry_trampoline`, SR L610-615), printed with `%q` of the record path (any correct shell quoting works; the text around it is fixed):

```
"$@"; rc=$?; rm -f -- <quoted record path>; if [ "$rc" -ne 0 ]; then printf '\n<prefix> exited with status %s. Press Enter to close this session.\n' "$rc"; read -r; fi; exit "$rc"
```

`Press Enter to close this session.` (`CCY_SESSION_ENDED_TEXT`, SR L599) is what `verify-restore` looks for (RG L877). The `<prefix>` in the trampoline is the **session's launcher prefix** (`ccy`/`cc`) taken from the record, passed by `ccy_tmux_start_detached` as `$CCY_TMUX_SESSION_PREFIX` — **a bug for a `cc` record restored by `ccy-sessions`**: the variable is not set by ccy-sessions (default `ccy`), so every restored session's trampoline says `ccy exited with status`, and the existing screens in R use that wording (R L972). Keep `ccy` unless phase 3 changes it; see 9.13.

### 4.7 `set-going` (L806-893 and helpers)

Takes no arguments (`'set-going' takes no arguments.`, 64). Sequence:

1. `floor = $CCY_RESTORE_COMPACT_FLOOR_TOKENS`.
2. Open `<manifest path>.set-going.lock` (`<state>/ccy/last-restore.set-going.lock`) for append and take an exclusive `flock` waiting up to `SET_GOING_WAIT` seconds; the descriptor is kept for the life of the process. Failures: `ERROR: could not open set-going's lock <lock>`; `ERROR: another set-going still holds <lock> after <WAIT>s; nothing was set going by this one.` (exit 1).
3. `ccy_restore_manifest_read` (6.2) failure -> `ERROR: this boot's restore recorded no sessions, so nothing can be set going; journalctl --user -u ccy-sessions-restore -b --no-pager | cat says why.`, exit 1 (R L1161-1162 asserts `journalctl --user -u ccy-sessions-restore`).
4. `boot = ccy_boot_id`; `RM_BOOT != boot` -> `ERROR: the restore manifest is from an earlier boot: this boot's restore failed before writing one (journalctl --user -u ccy-sessions-restore -b --no-pager | cat), so nothing is set going.`, exit 1 (R L1152-1154).
5. `work` = entries whose `going` is `pending` or `compacting`; none -> stderr `No session this boot's restore started is waiting to be set going.`, exit 0 (R L1075-1076).
6. `floor` not matching `^[1-9][0-9]*$` -> `ERROR: CCY_RESTORE_COMPACT_FLOOR_TOKENS is '<v>', not a whole number of tokens, so no restored session can be set going. ccy-sessions-set-going.service sets it from ccy_restore_compact_floor_tokens (play-claude-yolo.yml).`; each work entry is `leave_alone <name> <its going> no-compact-floor`; exit 1 (R L1115-1118 asserts both strings).
7. `deadline = SECONDS + SET_GOING_WAIT` (bash `SECONDS`: whole seconds since the shell started). Loop:
   - `ccy_tmux_list` failure -> `ERROR: the live session list could not be read, so no further session can be set going.`; each work entry `leave_alone ... session-list-unreadable`; exit 1 (R L1157-1158).
   - `is_live[name]`, `attached_to[name]` (the `#{session_attached}` count) from the listing.
   - For each work name in manifest order: if the entry is gone, skip; `pending` -> `set_one_going`; `compacting` -> `continue_after_compaction`. A `|| return 1` means the manifest write failed.
   - re-read the manifest (failure exit 1); recompute `work`; empty -> break; `SECONDS >= deadline` -> each work entry `leave_alone <name> <going> "<WAITED_ON[name] or (prompt-not-drawn | compaction-not-finished)>-after-<WAIT>s"`; break; else `sleep POLL`.
8. `LEFT_ALONE > 0` -> `ERROR: <n> restored session(s) were not set going; each is named above, and verify-restore reports it.`, exit 1. Else exit 0. Stdout is empty (R L1057).

`set_one_going(name, floor, live, attached)` (L700-751):

01. entry index; none -> return.
02. not live -> `leave_alone name pending session-not-running`.
03. `look_at(name, prefix, attached)` (L670-694): `capture-pane -p -e -t =<name>:` (error -> `ERROR: the screen of <name> could not be read: <text>` and the word `unreadable`); `ccy_restore_verdict(prefix, 1, plain(screen), "up")`: `DEAD <why>` -> `dead:<why>`; `WAITING-AT-PROMPT <p>` -> `waiting:<p>`; else if `attached > 0` -> `attached`; else `ccy_claude_screen_state(screen)` -> `ready|busy|typed|not-drawn`.
04. states: `unreadable`, `dead:*`, `attached`, `busy` (reason `busy-before-set-going`), `typed` (`input-box-not-empty`) -> `leave_alone name pending <reason>`; `unreadable` -> `screen-unreadable`; `dead:*` -> the why; `waiting:p` -> `WAITED_ON[name] = waiting-at-prompt-<p>` (kept pending, retried); `not-drawn` -> `WAITED_ON[name] = prompt-not-drawn`; only `ready` goes on.
05. transcript dir `ccy_transcript_dir(prefix, dir, ${CLAUDE_CONFIG_DIR:-$HOME/.claude})` and file `ccy_transcript_for(dir, resume)`; either fails -> `leave_alone name pending no-transcript` (the underlying `ERROR:` lines are printed too).
06. `conversation_owner(file, name)`: another entry with the same transcript whose going is `compacting|compact|continue` -> stderr `<name> shares its conversation with <owner>, which is set going; left alone.` and `leave_alone name pending shares-conversation-with-<owner>` (R L1173-1174).
07. `tokens = ccy_transcript_context_tokens(file)` failure -> `leave_alone name pending context-unreadable`.
08. `tokens >= floor` -> text = `SET_GOING_COMPACT`, going = `compact` (supervised yes) or `compacting` (supervised no); else text `continue`, going `continue`.
09. `at = EPOCHSECONDS` *before* typing; `type_into(name, text)`; failure -> `leave_alone name pending send-failed`.
10. stderr `set <name> going: <going> (context <tokens> tokens, compact floor <floor>, supervised: <yes|no>).` (R L1068); `going_set name pending <going> <at> <tokens> <file>`.

`continue_after_compaction(name, live, attached)` (L755-794): not live -> `leave_alone name compacting session-ended-while-compacting`; `ccy_transcript_compacted_since(transcript, at)` error -> `context-unreadable`; not yet -> `WAITED_ON = compaction-not-finished`, return; `look_at`: `ready` -> continue; `busy` -> stderr `<name> is already working after its compaction; nothing more typed.` and `going_set name compacting compact <at> <detail> <transcript>`; `not-drawn` -> `WAITED_ON = prompt-not-back-after-compaction`; `attached`/`typed` -> `leave_alone ... <state>-after-compaction`; anything else (`dead:x`, `waiting:x`, `unreadable`) -> `leave_alone ... "${state#*:}-after-compaction"` (so `waiting:quick-launch` becomes `quick-launch-after-compaction`, and `unreadable` stays `unreadable-after-compaction`). Ready: `at = EPOCHSECONDS`, `type_into name continue` (failure -> `send-failed-after-compaction`), stderr `set <name> going after its compaction: continue.`, `going_set name compacting continue <at> <detail> <transcript>`.

`type_into(name, text)` (L604-615): `tmux -L ccy send-keys -t =<name>: -l <text>` (failure `ERROR: typing into <name> failed: <text>`, return 1), `sleep SUBMIT_DELAY`, `tmux -L ccy send-keys -t =<name>: Enter` (failure `ERROR: pressing Enter in <name> failed: <text>`). The target **must** be `=<name>:` with the trailing colon (real tmux refuses `=name` for pane commands; G L205-218, R L99-105).

`going_set(name, expect, going, at, detail, transcript)` = `ccy_restore_manifest_update(boot, ...)` under the lock: re-read; entry must still be this boot's and still `expect`; set going/at/detail/transcript and rewrite. rc 3 (no longer applies) -> stderr `<name>: its entry in the restore manifest changed while it was being set going (a second restore this boot); that entry stands.`, return 0; other failure -> `ERROR: the restore manifest could not be updated for <name>.`, return 1.
`leave_alone(name, expect, reason)`: `LEFT_ALONE++`, stderr `left <name> untouched: <reason>`, `going_set name expect untouched "" <reason> ""`. (R L1066 asserts `left ccy-d untouched: launcher-exited` and `left ccy-w untouched: waiting-at-prompt-quick-launch-after-0s`.)

### 4.8 `verify-restore` (L900-1001)

`verify-restore [--wait SECONDS]`: `--wait` value must match `^[0-9]+$` else `--wait needs a whole number of seconds; got '<v>'.` (64, R L998-999); other -> `unknown verify-restore option '<arg>'.`.

1. `ccy_restore_manifest_read` failure -> exit 1 (`ERROR: no session restore has been recorded (<path> does not exist).` R L947-948).
2. `boot` mismatch -> `ERROR: the last session restore ran in an earlier boot, so it says nothing about this one; nothing restored these sessions since this machine started.`, exit 1 (R L953).
3. No entries -> stderr `This boot's restore brought up no sessions; nothing to verify.`, exit 0, stdout empty (R L957-958).
4. Loop with `deadline = SECONDS + WAIT`: `states = restore_states` (failure -> exit 1); break if no line lacks the suffix ` OK` or `SECONDS >= deadline`; break if `states == previous` **and** no line ends in ` STARTING` or ` SETTING-GOING`; `previous = states`; `sleep POLL`.
5. Print `states` on stdout (one `<name> <STATE>[ <detail>]` per manifest entry, manifest order). Any line not ending ` OK` -> stderr `Not every restored session is running and past its prompts; attach with ccy-sessions to answer or look.`, exit 1. Else 0.

`restore_states` (L900-955), once per pass: `ccy_tmux_list` (failure -> return 1); `session_containers` (failure return 1); for each engine in use (sorted), `<engine> ps --filter label=ccy=true --format '{{.Names}}'` (failure -> `ERROR: <engine> ps failed: <text>`, return 1; **this exact argv, R L144**); per entry: not live -> `live=0`, screen empty, container `-`; live -> `capture-pane -p -t =<name>:` (**no `-e`** here; failure -> `ERROR: the screen of <name> could not be read: <text>`, return 1 — the whole report fails, R L1003-1006); container state for a ccy entry whose session has an engine client: `up` if that container is in the engine's running list, else `starting`, else `-`;
`took` = `ccy_transcript_took_input_since(transcript, at)` only for going `compacting|compact|continue` (failure -> return 1), else `-`; `going = ccy_restore_going_state(going, at, detail, now, START_WINDOW, took)`; `state = ccy_restore_verdict(prefix, live, screen, container, going)`.

Pure helpers (golden cases RG L873-1031):

- `ccy_restore_verdict` (SR L1054-1096): not live -> `DEAD session-not-running`; last non-blank line (trailing whitespace trimmed, then leading) ending in `Press Enter to close this session.` -> `DEAD launcher-exited`; else if the last line **starts with** the text of a known prompt (CP L70-98, 26 prompts, **in that table order**, first match wins) -> `WAITING-AT-PROMPT <name>`; else prefix `cc` -> next; `ccy` with container != `up` -> `STARTING`; other prefix -> `DEAD unknown-launcher-<prefix>`; then by going word: `-` or `started` -> `OK`; `setting-going` -> `SETTING-GOING`; `failed:<r>` -> `NOT-SET-GOING <r>`; other -> `NOT-SET-GOING unknown-state-<going>`. Default going is `-`.
- `ccy_restore_going_state` (SR L1320-1344): `none` -> `-`; `pending` -> `setting-going`; `untouched` -> `failed:<detail or no-reason-recorded>`; `compacting` -> `failed:compact-not-started` if `took != yes` and `now - at >= window`, else `setting-going`; `compact|continue` -> `started` if took yes, `setting-going` while `now - at < window`, else `failed:<going>-not-started`; other -> `failed:unknown-going-<x>`. Bash arithmetic treats an empty `at` as 0.
- `ccy_claude_screen_state` (SR L1151-1182), `ccy_screen_plain` (SR L1110-1116: strip `ESC [ ... letter`), `_ccy_input_line_empty` (SR L1121-1140): input box = a line starting with `❯` whose previous line starts with ten `─` and with a later line starting with ten `─`; `busy` if any of the 4 nearest non-blank lines above the box contains `esc to interrupt`; `typed` unless the raw `❯` line is empty (only spaces / NBSP and an SGR sequence containing parameter `2` = the dim placeholder); else `ready`; no box -> `not-drawn`. Golden cases RG L938-965, R L1016, L1120, L1240.
- Transcripts (SR L1189-1308, golden RG L966-1031): dir `<dir>/.claude/ccy/projects/-workspace` for `ccy`; `<config home>/projects/<dir with every character that is not [A-Za-z0-9] replaced by '-'>` for `cc` (other prefix: `ERROR: no transcript directory is known for launcher '<p>'.`); `ccy_transcript_newest` = newest-mtime `*.jsonl` directly in the directory (errors `there is no transcript directory <d>` / `there is no transcript in <d>`); `ccy_transcript_for(dir, id)`: empty id -> newest; id must match `^[A-Za-z0-9-]+$` (`the resumed conversation id '<id>' is not a conversation id.`) and `<dir>/<id>.jsonl` must exist (`the resumed conversation <path> is not there.`). Context size = the value from the **last** main-thread (`isSidechain` not truthy) JSON line that is either an `assistant` message with an object `message.usage` and `message.model != "<synthetic>"` (sum of `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, each default 0) or a `system` `compact_boundary` with a numeric `compactMetadata.postTokens`; lines that are not JSON objects are skipped; none -> `ERROR: the transcript <f> records no context size.`; unreadable -> `ERROR: the transcript <f> could not be read: <text>`. `took_input_since(file, epoch)`: `yes` if any main-thread, non-meta (`isMeta` not truthy) `user` entry has a `timestamp` string (fraction stripped, `Z` form) with epoch >= `since`. `compacted_since`: same with a `system`/`compact_boundary` entry. Timestamps are UTC (comments at SR L1279-1280 about jq 1.6). Go replaces `jq`: **drop the jq dependency**; keep "bad timestamp is an error" and "bad JSON line is skipped".

## 5. External commands and their exact argv

| command (PATH lookup unless noted)                                                                                       | when                                                  | notes                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `tmux -L ccy list-sessions -F '#{session_name} #{session_attached} #{session_path}'`                                     | every list                                            | R's fake matches `*list-sessions*`                                                              |
| `tmux -L ccy list-panes -a -F '#{session_name} #{pane_pid}'`                                                             | session_containers, detail probe                      |                                                                                                 |
| `tmux -L ccy list-clients -t =<n> -F '#{client_tty} #{client_activity}'`                                                 | other terminals, is_detached, take over, after attach |                                                                                                 |
| `tmux -L ccy detach-client -s =<n>`                                                                                      | take over                                             | T L266 asserts `detach-client -s =ccy-a`                                                        |
| `tmux -L ccy set-hook -g client-attached <hook>`                                                                         | before attach                                         |                                                                                                 |
| `tmux -L ccy attach-session -t =<n>`                                                                                     | attach                                                | inherits stdin/stdout/stderr; T L296 asserts `attach-session -t =ccy-a`                         |
| `tmux -L ccy capture-pane -p [-e] -t =<n>:`                                                                              | set-going (`-p -e`), verify (`-p`)                    | colon required                                                                                  |
| `tmux -L ccy send-keys -t =<n>: -l <text>` / `... Enter`                                                                 | set-going                                             |                                                                                                 |
| `tmux -L ccy kill-session -t =<n>`                                                                                       | Ctrl-X                                                | fake in T has no `kill-session` (never exercised)                                               |
| `ps -ww -eo pid=,ppid=,args=`                                                                                            | probes                                                | fake `ps` ignores argv                                                                          |
| `<engine> ps --filter label=ccy=true --format <fmt>` ; `<engine> inspect --format '{{.Name}} {{.State.Pid}}' <names...>` | probes, verify                                        | `engine` = the word found in the ps table (`podman`/`docker`)                                   |
| `<engine> exec <container> /workspace/.claude/hooks-daemon/bin/hooks-daemon signal ...`                                  | notify/reboot route                                   |                                                                                                 |
| `<dir>/.claude/hooks-daemon/bin/hooks-daemon signal ...` (absolute path)                                                 | notify/reboot, host route                             | R's fake derives its logged `cli[<dir>]` from `$0`, so it must be run by that path              |
| `busctl --system call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanReboot`           | reboot                                                |                                                                                                 |
| `systemctl reboot`                                                                                                       | reboot                                                | argv exactly `reboot`                                                                           |
| `git rev-parse --is-inside-work-tree`                                                                                    | project_here                                          | real git in T                                                                                   |
| `systemd-run`, `systemd-escape -- <name>`                                                                                | restore                                               | A's fakes: `systemd-run` execs everything after `--`; `systemd-escape` echoes its last argument |
| `getconf CLK_TCK`, `sleep`                                                                                               | probe, countdown                                      | replace by library calls (no fake)                                                              |
| `fzf` (argv as 3.7)                                                                                                      | pickers                                               |                                                                                                 |
| `jq`                                                                                                                     | transcripts                                           | drop                                                                                            |
| `flock`                                                                                                                  | set-going `-w`, manifest `-w 60`                      | use `syscall.Flock`                                                                             |

## 6. Files and formats

### 6.1 Registry (shared with the launchers)

Directory (SR L54-71, `registry.Dir`): `$CCY_STATE_DIR/sessions` if set, else `${XDG_STATE_HOME:-$HOME/.local/state}/ccy/sessions`; a relative `XDG_STATE_HOME` or base is an error with the sentences in `registry.go` L43 and L53. One file per session, `0600`, directory `0700`, name = session name (no `/`).
Record: header `ccy-session-record 1`, then `name=`, `dir=`, `launcher=`, `prefix=`, `restore=yes|no`, optional `compose=started|declined|running`, then repeated `arg=`. `registry.ReadFile`/`Write`/`List`/`Remove`/`SetRestore` already implement it (golden RG L227-300, L627-652); ccy-sessions uses `Remove` (Ctrl-X) and, for restore, a reader that **reports each unreadable record and counts it as a failure**: `Store.List` returns `Unreadable` names but drops the reason, so restore needs a variant that surfaces the reader's `ERROR:` text (the bash reader prints `session record <file> ...`). Record files named `*.tmp.*` are in-flight writes and ignored (`List` does that).

### 6.2 Restore manifest (SR L682-974)

- Path: `<registry dir without "/sessions">/last-restore`; lock `last-restore.lock` (waited 60 s, then `ERROR: the restore manifest's lock <lock> was not released within 60 s; nothing was written.`); set-going lock `last-restore.set-going.lock` (4.7). Written whole (`0600`, tmp `<path>.tmp.<pid>`) then renamed. Directory created `0700`.
- Format: line 1 `ccy-restore-manifest 2`; then `boot=<boot id>`; then per session, in order, exactly these nine lines: `name= prefix= dir= resume= supervised=yes|no going=none|pending|compacting|compact|continue|untouched at=<digits or empty> detail= transcript=`.
- Reader is strict (SR L872-962): a first line starting `ccy-restore-manifest ` but not the current header -> `ERROR: the restore manifest <path> is '<line>', written by another version of ccy-sessions; this one reads 'ccy-restore-manifest 2'. The next restore (at the next boot) writes it again.` (RG L832-836 asserts `another version of ccy-sessions`); another header -> `... does not start with '...' and is not read.`; a line without `=`; a key other than the expected next one (`has '<k>' where '<e>' was expected and is not read.`); bad `supervised`, `going` or `at`; empty file; no boot; part-way entry; blank lines ignored; a missing file -> `ERROR: no session restore has been recorded (<path> does not exist).`; unreadable -> `... cannot be read.`.
- `ccy_boot_id`: first line of `$CCY_BOOT_ID_FILE` or `/proc/sys/kernel/random/boot_id`; failure -> `ERROR: this boot's id could not be read from <file>.`.
- `_ccy_restore_manifest_merge` (the restore's write): no manifest, an unreadable one, or one from an earlier boot starts empty; an entry whose going is `keep` takes the existing entry of that name (else `none`); the rest are written as given. `ccy_restore_manifest_update` (set-going's write): re-read under the lock, require same boot and `expect` going, set four fields, rewrite (re-stamps the current boot id). Values with a newline are refused.
- `ccy-sessions restore` and `set-going` are the only writers today, so after the port the Go side is the only production writer. R and RG still write fixtures through the bash lib (R L942), so the Go reader and writer must stay byte-compatible with it.

### 6.3 Other files read

`~/.config/systemd/user/default.target.wants/ccy-sessions-restore.service` (existence through symlinks); transcripts (4.8); `/proc/<pid>/stat`; `/proc/sys/kernel/random/boot_id`; the passphrase file (checked, never read).

## 7. Data model (for the Go types)

- `tmuxccy.Session{Name, Attached int, Path}` (exists), `Client{TTY, Activity int64}` (exists).
- `Row{Session, State, CPU, Network, Token, Keys, Dir}` (display); `Probe{Network, Token, Keys, CPU map[string]string}`.
- `Route{Cmd []string, Root, Via string}`, `Project{Dir, Sessions []string, Engine, Container}`.
- `registry.Record` (exists). `restore.Entry{Name, Prefix, Dir, Resume, Supervised bool, Going string, At int64 (or empty), Detail, Transcript}` and `restore.Manifest{Boot string, Entries []Entry}`; `Going` is an enum of the six words (+ `keep` only as input to the merge).
- `screen.State` enum `Ready|Busy|Typed|NotDrawn`; verdict as a struct `{State, Detail}` rendered `STATE[ detail]`.
- Errors: a small set of sentinel/typed errors so `Run` can map them to exit codes (usage 64, cancelled 130, refusal 1, child rc).

## 8. Mapping onto `src-go/internal/` and the proposed layout

### 8.1 What exists and what each flow uses

| flow                                   | existing package (functions)                                                                                                                 | missing                                                                                                                                                                |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| exit codes, `~` form, stderr lines     | `term`: `ExitOK/Failure/Usage/Cancelled`, `Term.Printf/Say`, `AbbrevHome`                                                                    | `PrintError` colours only on a tty (bash always colours: 9.8). No stdout writer helper                                                                                 |
| fzf picker, confirm, header            | `ui`: `Picker.Pick/Confirm`, `Header`, `ErrLeave`, `ExitRow`                                                                                 | typed "fzf not installed" error that is **not** `ErrLeave` (9.1). No gum use: ccy-sessions asks nothing through gum                                                    |
| running programs, fakes                | `execx`: `Runner`, `OS`, `Fake`, `Cmd` (Stdin/Stdout/Stderr/Env/Dir), `ExitError`                                                            | merged stdout+stderr capture (`2>&1`: probes, `busctl`, engine ps, `exec`); remove-from-environment (restore passphrase); `Exec` seam for Ctrl-N (replace the process) |
| tmux list/clients/detached/hook/attach | `tmuxccy`: `Tmux.List/Clients/IsDetached/InstallHook/Attach`, `SingleAttachHook`, `Session/Client/Outcome`, `RefusedError`, `ErrSessionGone` | see 8.2                                                                                                                                                                |
| `git rev-parse --is-inside-work-tree`  | `gitx.ProjectHere`                                                                                                                           | nothing                                                                                                                                                                |
| registry records, remove               | `registry`: `Dir/Locate/Store.Get/List/Write/SetRestore/Remove/ReadFile`                                                                     | see 8.3                                                                                                                                                                |
| systemd                                | `systemd`: `Systemd{Runner}` with `HomeserverUnits/HomeserverState/LaunchElement`                                                            | `Reboot` (`systemctl reboot`), `RunScope`/`Escape` (systemd-run, systemd-escape), `WantsEnabled` (`going_down_kind`)                                                   |
| sudo                                   | `sudo`                                                                                                                                       | **not used**: nothing in ccy-sessions calls sudo (it only *advises* `sudo /usr/local/bin/reboot-with-update`)                                                          |
| agentbus, declare, marker, teams       | -                                                                                                                                            | **not used**. `teams.App.ccyInstalled` (teams/app.go L73-84) is the CCY_LIB guard; extract it (`internal/ccylib` or into `term`) and share it                          |

### 8.2 `tmuxccy` additions

- `Tmux.Socket` field defaulting to `ccy` (needed to port G against a private socket; `Socket` is a const today, `Run` hard-codes it).
- `OtherTerminals(ctx, name) (words string, err)` + `ErrSessionGone` mapping (returns the rc-2 case), `ClientWords(clients, now)`, `IdleWords(secs)` (pure), `TakeOver(ctx, name, waitTenths) (Outcome, error)` with the exact messages of 3.5; `Attach` already covers 3.4 but its tmux-failure path returns the raw error where bash prints the "open in another terminal, or gone" sentence (9.4).
- `Panes(ctx) ([]Pane{Session, PID}, error)` (`list-panes -a`), `CapturePane(ctx, name, escapes bool)`, `SendKeys(ctx, name, text, literal bool)` / `SendEnter`, `KillSession`.
- `ProjectSessions(ctx, cwd, prefix)`, `NextName(ctx, prefix, project)`, `Insulate(...)`: **not needed by ccy-sessions**, needed by phase 3 `ccy` (TS L63-84, L856-952, L995-1000). Plan them in the same package.
- `StartDetached(ctx, name, dir, cmd []string, ...)` (TS L965-990) using `systemd` helpers, the registry trampoline and `SingleAttachHook()`.
- Pure join code with the N golden cases (N L58-247): `SessionContainers(panes, ps)`, `SessionPIDs(roots, ps)`, `CPUWords(...)`, `NetworkWord`, `IdentityWords`, `CPUSample` (`/proc`), plus `Rows`-support types. Place these in a new `internal/sessprobe` (they are about processes and engines, not tmux) — see 8.4.

### 8.3 `registry` additions (and new sibling packages)

- `ReplayArgs(prefix, args)`, `FlagClass(word)`, the four flag lists (SR L122-153, L165-233; phase 3 and `cc`; **not needed by ccy-sessions**); `RestoreArgs(compose, prefix, args)`, `ResumeID(args)`, `Supervised(prefix, args)` (needed by restore); `Trampoline(recordPath, prefix)`; `ForgetNetwork`, `RecordSSHKey`, `RecordCompose` (launcher side, phase 3).
- A reader that returns the reason a record is unreadable per file (6.1).
- New `internal/restore`: manifest types, `Read/Write/Update/Merge` with the two locks (use `syscall.Flock`; a timed wait is a `LOCK_NB` poll every 100 ms), `BootID`, `Restore(opts)` (SR L1421-1529), `GoingState`, `Verdict`.
- New `internal/screen`: `Plain`, `ClaudeState`, `KnownPrompts` (CP L39-98; shared with the launcher in phase 3 — keep the table in one Go file and have a test compare it with `common-pure.bash`), `EndedText`.
- New `internal/transcript`: `Dir`, `Newest`, `For`, `ContextTokens`, `TookInputSince`, `CompactedSince`.

### 8.4 Layout

```
src-go/cmd/ccy-sessions/main.go        # os.Exit(sessions.NewApp().Run(os.Args[1:])); no ldflags needed
src-go/internal/sessions/
    app.go          # App{Runner, Stdin/Stdout/Stderr, Getenv, Clock, Sleep, IsTerminal...}; Run; dispatch; guard; usage_error
    usage.txt       # L2-L109 verbatim, //go:embed
    table.go        # rows(), heading, list command, row formatting
    picker.go       # loop, keys, confirm_end, attach_here, confirm_take_over, start_ccy_here
    routes.go       # session_containers, set_route, check_projects, deliver_signal
    notify.go       # cmd_notify, going_down_kind
    reboot.go       # cmd_reboot, reboot_permitted, countdown, withdraw, signals
    setgoing.go     # cmd_set_going, set_one_going, continue_after_compaction, look_at, type_into
    verify.go       # cmd_verify_restore, restore_states
    restore.go      # cmd_restore -> restore.Restore
src-go/internal/sessprobe/              # pure process/engine joins + CPU (TS L374-743)
src-go/internal/restore/  screen/  transcript/    # 8.3
src-go/internal/tmuxccy/ registry/ systemd/ execx/ ui/ term/    # additions above
```

`App` must take `Clock` (`Now() time.Time`, monotonic `Since`), `Sleep(d)`, and a `Signals` channel so the reboot countdown, set-going deadline and verify `--wait` are testable without real waits; R sets the `CCY_SESSIONS_*` env vars for the same purpose, which the binary must also honour.

## 9. Known bugs and oddities: fix, do not port

01. **Missing fzf exits 0.** `ccy_tmux_pick` prints the install hint and returns 1; every caller treats non-zero as Esc (L1188-1190, TS L787). Go: make it a failure, exit 1, same hint (CLAUDE.md fail-fast).
02. **Any fzf failure is Esc** (an fzf crash, exit 2, a bad option): `ui.Picker.Pick` also maps `exitCode > 0` to `ErrLeave`. Keep 130/1 as leave; surface other codes (2) as errors.
03. **Daemon CLI stdout pollutes `reached`.** `reached="$(deliver_signal ...)"` (L558) captures whatever the CLI prints on stdout in front of the count, giving `Rebooting in N minute(s); <cli output><n> project(s) warned.`. Send child stdout to stderr (or discard, as `notify` does with `>/dev/null`).
04. **`ccy_tmux_attach` collapses a tmux failure into "open elsewhere, or gone"** (TS L117-120; `ccy_tmux_is_detached` returns 2 for a real error and 1 for gone/busy; the caller tests only `!`). Go `Attach` returns the raw error instead. Pick one: report the real tmux error.
05. **`confirm_end` aborts the picker on an already-gone session**: `kill-session` is unguarded under `set -e` (L1085), so a session that ended since the list was drawn exits the program with tmux's status and tmux's raw message, leaving its record in place. Treat `can't find session` as already-ended (remove the record, say so).
06. **`attach_here` prints "taken or ended just now" for rc 1** too (L1101-1103), including a real attach/set-hook failure. Distinguish.
07. **MAX_TRIES counts** a Ctrl-N outside a project and an Enter on an open-elsewhere row as tries, and ends the picker with "Nothing changed" (exit 1) after three of them; Ctrl-X and a declined take-over never count (L1203-1209). Keep or redefine deliberately (also in the teams spec, 8.04).
08. **`print_error` always emits colour and interprets backslashes** (`echo -e`, CP L25-27); `term.PrintError` already colours only on a tty and prints literally. Tests assert substrings only. Port with the Go behaviour.
09. **`open elsewhere` is detected by substring of the row text** (L1212) so a project directory or session name containing that phrase misclassifies it. Carry the state in data, not text (the picker rows are re-read from fzf's output only to recover the name; use an index or `--with-nth` as in the teams spec 8.20).
10. **`~` abbreviation is a bare prefix replace** (`${3/#${HOME}/\~}`, TS L770; `${PWD/#${HOME}/\~}`, L1153): with `HOME=/srv/u`, the path `/srv/u2/x` becomes `~2/x`. Use `term.AbbrevHome`. With an empty `HOME`, bash does nothing; `AbbrevHome` also does nothing.
11. **Engine and `--name` parsing** (TS L386-413) takes the first `--name` anywhere on any `podman`/`docker` command line, including after the image (`podman run ... img claude --name foo`). Keep the observable rule for the golden cases (N L58-132) but stop at the first non-option word of `run`/`create` if that does not change them.
12. **`restore ''`** is accepted by `cmd_restore`'s case (`""`) and rejected by the library (L589, SR L1425); make it a usage error. **`ccy-sessions ''`** is the picker.
13. **Trampoline text names the wrong launcher for `cc` sessions** restored by ccy-sessions (`ccy exited with status`; 4.6). Verify-restore matches only the second sentence, so it is cosmetic; fix in the shared Go trampoline when phase 3 takes over, and update R L972 / RG L877 then.
14. **Non-determinism**: bash associative-array order in the engine loops (TS L678, L699, L916) and the glob order of registry files (locale-dependent). Sort. The restore's start order then follows the registry file names.
15. **`SECONDS` is whole seconds since the shell started**, so deadlines can fire up to a second early (L846, L984). Use a monotonic clock; `WAIT=0` must still run exactly one pass (R relies on it, R L1056, L1127).
16. **`flock -w` with `SET_GOING_WAIT` shared by lock wait and prompt deadline** (L820, L846): worst case twice the documented wait. Keep or split deliberately; the unit comments say "20 minutes by default".
17. **Probe messages differ in wording** between `session_containers` (L213) and `ccy_tmux_detail_rows` (TS L665) for the same `ps` failure. Harmless; unify unless a suite asserts one (none does).
18. **`continue_after_compaction` reasons** lose the `waiting-at-prompt-` / `dead:` prefixes (L784, 4.7). They appear in `verify-restore` as `NOT-SET-GOING <detail>`. Port verbatim now; normalise only with a deliberate test change.
19. **`ccy_transcript_dir` for `cc`** replaces each *character* not in `[A-Za-z0-9]` with `-`; bash counts bytes in a C-locale unit (the set-going unit has no `LANG`) and characters in a UTF-8 shell, whereas Claude uses UTF-16 code units. Decide one rule (UTF-16 units) and test with a non-ASCII directory.
20. **`CPU` sample can be zero-length** (`EPOCHREALTIME` difference of two reads with `CCY_CPU_SAMPLE_SECONDS=0`) and then fails with the "needs time to pass" error and `unknown` columns; with a nanosecond monotonic clock the case disappears, but keep the check.
21. **Missing manifest entries / wrong-boot** is reported by three different sentences (4.7, 4.8, 6.2). Keep verbatim; tests assert fragments (R L948, L953, L1154, L1161).
22. **`reboot` sends the second warning through a fresh `check_projects`** (so the probes run again and the project list can change between the two warnings) but `withdraw` also re-lists; this is intentional (new sessions get warned) and should be documented, not "fixed".
23. **Stale deployment comment** in the play (L829-831) and a stale `reboot_permitted` advice path (`/usr/local/bin/reboot-with-update`) hard-coded in a message: keep, it is asserted (R L560).
24. **Dry-run runs live probes** (`signal --help` in every project): intentional (R L460-462), documented only in a code comment.

## 10. Test seams: driving a Go binary with the existing suites unchanged

What a binary must do so R, T, A keep working **unchanged** (the "keep" list), and what cannot work (the "must change" list).

### 10.1 Keep

1. Resolve every tool by PATH (`tmux`, `ps`, `podman`/`docker`, `busctl`, `systemctl`, `fzf`, `git`, `systemd-run`, `systemd-escape`); only the daemon CLIs are absolute paths. Run the engine by the **name found in the ps table** (R's `podman` fake).
2. Pass `-L ccy` first to tmux (T's fake does `[ "$1" = "-L" ] && shift 2`) and issue **only** these tmux subcommands: `list-sessions`, `list-panes`, `list-clients`, `detach-client`, `attach-session`, `set-hook`, `display-message` (T) / `list-sessions`, `list-panes`, `capture-pane`, `send-keys` (R). Any other subcommand is exit 97 and a test failure by design (T L90-91, R L53-55).
3. fzf exactly as in 3.7: `--expect=` only when keys exist (T's fake prints a key line iff `$*` contains `--expect`), rows on stdin including the `Exit` row, chosen-row lookup by first word for session rows and by prefix `Yes, ` for confirms; header text inside argv (T asserts fragments of `$*`, T L364-373, L408).
4. Environment: honour `HOME`, `PATH`, `CCY_LIB`, `CCY_STATE_DIR`, `CCY_BOOT_ID_FILE`, `CCY_CPU_SAMPLE_SECONDS`, `CCY_TMUX_TAKE_OVER_WAIT_TENTHS`, all `CCY_SESSIONS_*`, `CCY_RESTORE_COMPACT_FLOOR_TOKENS`, `CCY_RESTORE_SSH_PASSPHRASE_FILE`, `CLAUDE_CONFIG_DIR`, `TMUX_TMPDIR`-style inheritance (G).
5. Terminal handling: `script -qec <tool>` gives a pty on 0/1/2 (T L355); the `-t 0`/`-t 1` test must be on the real fds. `$PWD` is the cwd T passes (`cd "$SCRATCH"`).
6. stdout/stderr split of section 0, since R reads `2>/dev/null` for `--list` and `2>"$VR/stderr"` for verify/set-going (R L309-310, L909, L1024).
7. Signals: `kill -TERM <pid>` during the countdown must end the process with 130 after sending `reboot-cancelled` (R L638-643); a TERM sent by `systemctl reboot`'s fake to its caller must be ignored (R L165-168, L776-782).
8. Exit codes of 1.3, exactly, including `rc` of `systemctl reboot` (R L534).

### 10.2 Must change (bash-internal dependencies)

1. **R L1013** extracts the compact text with `awk -F'"' '/^SET_GOING_COMPACT=/ { print $2 }' "$TOOL"`: impossible on a binary. Replace with the same awk against the Go source constant, or compare with the text in this spec.
2. **R L896-898, L942, L1040, L1069, L1194, L1217** source `common-pure.bash`/`session-registry.bash` and call `ccy_restore_manifest_write/read`, `ccy_transcript_dir`, and read `RM_*` / `CCY_PROMPT_*` / `CCY_SESSION_ENDED_TEXT`. These remain valid fixtures while the bash lib exists (the Go side must read what that writes and write what it reads), but they stop testing the port. Port them to Go unit tests in `restore`, `screen`, `transcript` (the golden values are in RG L806-1031).
3. **R L259** symlinks `$TOOL` as `~/.local/bin/ccy-sessions` for `shutdown-with-update` (R L646-880): works with a binary if `TOOL` is the built path (needs a build step in the suite, as in the teams spec).
4. **T L225-348** source `tmux-session.bash` and test `ccy_tmux_idle_words/client_words/other_terminals/take_over/attach/current_session` directly (and `declare -F` them, L232-237). These are library tests, not binary tests: port to `tmuxccy` Go tests with a fake `Runner`; keep the bash ones while the lib exists. T L350-423 (the picker) are binary-driven and unchanged.
5. **G (`test-ccy-tmux-targets.bash`)** scans `ccy-sessions` and `tmux-session.bash` **source text** for `ccy_tmux <sub> ... -t "<template>"` (G L28-30, L115-125). With a binary, nothing is scanned from the tool, and `check "a capture-pane target was among them"` (G L183-186) **fails**: `capture-pane`, `send-keys` and `kill-session` exist only in ccy-sessions today. The real-tmux check must move to Go: an integration test (`go test`, skipped without tmux) using a private socket (hence `Tmux.Socket`) and the actual `Tmux` methods, plus the bare-`=name` refusal case (G L188-219).
6. **A L428-495** sources the libs, **redefines `ccy_tmux_list`** (L442) and calls `ccy_registry_restore` in-process. It tests the lib, not the tool. To test the Go binary it must run `ccy-sessions restore` with a fake `tmux` that answers `list-sessions` with nothing **without logging it** (today's fake logs every call, which would add a line to `TMUX_ENV_LOG` and break the `unset unset` check at A L488-489).
7. `test-self-update-cycle.bash` fakes `ccy-sessions` entirely (L161-162); nothing to change. `test-ccy-teams-command.bash` L676 only greps text.
8. RG and N source the libs only (no `$TOOL`): they keep passing as long as the libs exist, and are the cases to port (RG: replay L101-204, record L227-300, trampoline L307-372, restore_args L373-402, forget_network/ssh_key/compose L403-626, restore L653-805, manifest L806-872, verdict/going/screen/transcript L873-1031, known prompts L1032+; N: containers L58-132, network word L133-166, identity L167-190, CPU L191-247, row L248-288, probe L289-463).
9. R's fake `tmux` answers `*list-sessions*` for any call whose text contains that word and `capture-pane` with the `=name:` check; a Go `Run` that wraps errors (`tmux list-sessions: tmux exited 1: ...`) is fine since matching is on stderr substrings (`no server running`, `No such file or directory`, `can't find session`), which `tmuxccy` already does.

## 11. Strings asserted by the suites (keep verbatim)

R: `notify`/`reboot`/`restore` in help (L290); `needs a terminal` (L293); `No CCY or cc sessions` (L366); `would signal shutdown-warning --minutes 5` (L389); `would signal`, `would reboot` (L435-436); the per-project `cli[<dir>] signal <kind> --minutes N --all-sessions --project-root <dir>` (L344-346); `(in container c_yolo)` (L486, L493); `would signal reboot-warning --minutes 2 to <dir> (in container c_yolo)` (L504); `staying up` (L523); `still expect the reboot` (L530); `the reboot failed` (L538); `CanReboot: <word>` (L559); `sudo /usr/local/bin/reboot-with-update` (L560); `could not ask logind`, `Failed to connect` (L570); `Ctrl-X`, `'install'` (L587); `could not be read` (L604); `Reboot cancelled` (L643); `Left out`, `ccy-s  in <dir>` (L803); `nothing to restore` (L887); `no session restore has been recorded` (L948); `earlier boot` (L953); `ccy-b WAITING-AT-PROMPT quick-launch` (L977); `cc-c WAITING-AT-PROMPT token-select` (L979); `ccy-d DEAD launcher-exited` (L980); `ccy-e DEAD session-not-running` (L981); `Not every restored session` (L983); `screen of ccy-b could not be read` (L1006); `left ccy-d untouched: launcher-exited` (L1066); `waiting-at-prompt-quick-launch-after-0s` (L1066); `set ccy-a going: compact (context 208525 tokens, compact floor 150000, supervised: yes)` (L1068); `left <n> untouched: <reason>` for `no-compact-floor`, `busy-before-set-going`, `prompt-not-drawn-after-0s`, `no-transcript`, `context-unreadable`, `send-failed`, `session-not-running`, `session-list-unreadable`, `attached`, `input-box-not-empty`, `compaction-not-finished-after-0s` (L1118-1243); `ccy-a-2 shares its conversation with ccy-a, which is set going; left alone.` (L1174); `NOT-SET-GOING shares-conversation-with-ccy-a` (L1182); `compact-not-started`, `continue-not-started` (L1090); `busy-before-set-going` as a NOT-SET-GOING detail (L1098); `journalctl --user -u ccy-sessions-restore` (L1162); `ccy_restore_compact_floor_tokens` (L1118).
T: `Ctrl-T take over`, `ctrl-t` (L364-365); `/dev/pts/3 (idle 3 h)`, `keeps running` (L372-373); `Ctrl-T takes it over` (L389); `Nothing changed` (L416); `A take-over was tried on ccy-a ccy-b ccy-c` (L423); `/dev/pts/4 (idle 10 min)` (L408); library messages `has ended` (L275), `server exited unexpectedly` (L282), `was not attached` (L336), `ended` (L328).
A: `CCY_SESSION_RESTORE=1 CCY_RESTORE_SSH_PASSPHRASE_FILE=<path>` on the pane command of ccy sessions only (L481-486); `play-claude-yolo.yml` and the file name in the failure (L495-496).
RG: `another version of ccy-sessions` (L836).

## 12. Porting order and risks

1. `term`/`ui`/`execx` gaps (merged capture, env removal, exec seam, fzf-missing error) and extract the CCY_LIB guard.
2. Pure packages first, with the golden cases ported from RG/N/T: `sessprobe`, `screen`, `transcript`, `registry` additions, `restore` (manifest, verdict, going state, restore_args). These carry most of the logic and are fully testable without a terminal.
3. `tmuxccy` additions (take-over, words, panes, capture/send-keys, start-detached) with a `Fake`.
4. `sessions`: `--help`, `--list`, `notify`, `reboot`, `restore`, `set-going`, `verify-restore` (all headless, covered by R), then the picker (covered by T).
5. Build/deploy: replace the `copy` task with whatever phase 1 chooses for ccy-teams (no ROOT_DIR needed here), keep the unit paths; bump nothing in `claude-yolo` (the launcher is untouched) unless the shared libs change (CCY version bump is mandatory then).

Biggest risks: (a) the three suites that read bash source or source the libs (10.2) lose coverage silently or fail on a binary; (b) `reboot`'s signal/trap/exit-handler semantics (withdraw after a partial warning, ignore TERM during `systemctl reboot`) are easy to get subtly wrong and are the safety net for agents; (c) byte-compatibility of record, manifest and tmux session shape with the bash launcher until phase 3; (d) exact tmux/engine argv, since the fakes reject anything unexpected and real tmux rejects `=name` without a colon.
