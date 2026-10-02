# Research: extending the tmux F12 menu (first item: hot-swap this ccy session's Claude token)

Read-only research. Nothing was edited, deployed or committed. No tmux server is reachable
from inside the CCY container (`tmux` is not installed in the image, `$TMUX` is unset), so
nothing here comes from a live `tmux list-keys`. Every claim cites the repo. tmux behaviour
not visible in the repo is marked **[tmux man page]** and should be confirmed against the
installed version before an implementation relies on it. The repo's record of the version
is tmux 3.7c, measured on a deployed box (`CLAUDE/Plan/Completed/00105-tmux-sessions-single-key-menu/JOURNAL/00105-Journal-26-09-08.md:80,86`).

---

## 1. Where the F12 menu comes from

**It is ours: a plain `display-menu` binding in this repo's system-wide tmux config.** It
is not a tmux plugin, not Claude Code, not the hooks daemon and not the ccy supervisor.

- `files/etc/tmux.conf:23-30` holds the whole menu:
  `bind -n F12 display-menu -T " sessions " -x C -y C` followed by five items: New session `n`,
  Rename session `r`, Switch session `s`, a separator, Detach `d`, and Kill this session `k`
  (behind `confirm-before`).
- The same file sets `status off` (`:14`), `mouse on` (`:12`), and a window title that
  advertises F12 (`:20-21`).
- The supervisor `.claude/ccy/claude-supervise.py` has no tmux binding. Its only mention of
  tmux is a comment at about `:4689` on how pty drivers behave. The hooks-daemon source has
  no tmux references at all.
- No user-level tmux config is deployed. `grep` finds no `~/.tmux.conf` or
  `~/.config/tmux/` under `files/home`. So `/etc/tmux.conf` is the only config a server loads.
- History: Plan 00105 (`CLAUDE/Plan/Completed/00105-tmux-sessions-single-key-menu/`). The
  owner settled on ONE key and tmux's native `display-menu` after turning down other designs
  (journal `:29-36`).

**The tmux server runs on the HOST, not in the container.** That means a menu item can run
host-side commands.

- `files/var/local/claude-yolo/lib/tmux-session.bash:2-17`: before its first prompt, the
  launcher re-executes itself inside tmux on a dedicated socket, `tmux -L ccy` (`:31`, `:38-40`).
  The server's cgroup is a `ccy-tmux-*.scope` under `systemd --user`
  (`:544-569`, `systemd-run --user --scope ... tmux -L ccy new-session -d ...`).
- It is called from the launcher at `files/var/local/claude-yolo/claude-yolo:925-936`, before
  the container exists. The container gets its name later, at `:2977`
  (`get_next_container_name "$PROJECT_NAME" "yolo"`), and starts at about `:3232`
  (`container_cmd run ... --name "$CONTAINER_NAME" --label "ccy-token=$CCY_LABEL_TOKEN" -e CLAUDE_CODE_OAUTH_TOKEN ...`).
  It is not `exec`'d, so the pane's process tree is: pane shell, then the launcher, then
  `podman run --name <container>`.
- `cc` (the host-side Claude launcher) uses the same server with the session prefix `cc`
  (`files/var/local/claude-code/cc:60-63`).
- Plain `tmux new -s NAME` sessions use the DEFAULT socket, which is a separate server.
- **Every one of these servers loads `/etc/tmux.conf`.** tmux's default config search
  [tmux man page] applies to `-L ccy` as well, so F12 behaves the same everywhere
  (`docs/tmux-sessions.md:42-49`). A menu item must therefore check which server and session
  it was opened from before doing anything specific to ccy.
- Commands that `run-shell` or `display-popup` start run as the user who owns the server, on
  the host, with host `podman` and host files available. That is exactly what the token
  item needs, because tokens live on the host (`files/var/local/claude-yolo/lib/token-management.bash`).

## 2. How it is deployed, and whether anything upstream overwrites it

- Playbook: `playbooks/imports/play-tmux-sessions.yml`, imported from
  `playbooks/playbook-main.yml:42`. It does three things:
  - installs tmux (`:15-18`);
  - deploys the config with `ansible.builtin.copy` from `files/etc/tmux.conf` to
    `/etc/tmux.conf`, `root:root 0644` (`:20-26`);
  - parses the file against a throwaway server, so a typo fails the play:
    `tmux -L config-check -f /etc/tmux.conf start-server \; kill-server` with `TERM` forced (`:28-37`).
- Nothing upstream owns this file. The hooks daemon replaces only `CLAUDE/core/` and its own
  tree. The ccy plays do not touch tmux config. The play is the only owner, and a hand-edit
  of `/etc/tmux.conf` on a host would be overwritten on the next run, as IaC intends.
- Acceptance checks only test that the file exists and tmux is present
  (`files/home/.local/share/vmtest/guest-acceptance-desktop.bash:98-102`,
  `guest-acceptance-server.bash:142-146`). They do not check the menu items, so changing the
  menu breaks no test.
- **Deployment trap: running servers do not reload.** `copy` replaces the file on disk, but a
  tmux server reads its config only at start. ccy sessions are long-lived, and on hosts that
  opted in they are restored at boot by `files/home/.config/systemd/user/ccy-sessions-restore.service`.
  So a changed F12 binding reaches an existing ccy server only after
  `tmux -L ccy source-file /etc/tmux.conf`, or after that server restarts. Any design that
  needs `tmux.conf` edited for every new item has this problem every time. The recommended
  design below has it once.

## 3. Design options for extensibility

### A. Assemble a static `display-menu` into tmux.conf at deploy time (Jinja template over a list variable)

- Good: everything is visible in one file, and it costs nothing at runtime.
- Bad: every new item means editing the central list or template, so a feature's play cannot
  ship its own item (DRY and ownership). Every addition needs every running server reloaded.
  Nested quoting (conf parser, then command parser, then the shell) gets worse with each item,
  as the existing `command-prompt ... \"new-session -s '%%'\"` lines already show.

### B. `source-file /etc/tmux.conf.d/*.conf` (tmux expands globs in `source-file`, and `-q` tolerates no matches [tmux man page])

- Good: tmux-native drop-ins.
- Bad: a drop-in can bind keys or set options, but it **cannot add an item to an existing
  `display-menu`**. The menu is one command with all its items inline. Each drop-in would need
  its own key, which brings back the multi-hotkey design the owner rejected. A single
  submenu would need a central list anyway. It also still needs a reload for each addition.

### C. One static submenu entry in F12 that calls a generator; items are drop-in files read when the menu opens (RECOMMENDED)

- `tmux.conf` gains ONE item, for example `"Fedora Desktop ▸" f "run-shell -b '<menu-runner> menu #{q:socket_path} #{q:client_name} #{q:pane_id}'"`.
  `run-shell` expands formats in its command [tmux man page], so the runner is told which
  server, which client and which pane it was opened from. It does not have to guess.
- The runner reads `/etc/tmux-menu.d/*` **each time it is pressed**. It builds the
  `display-menu` argv as a **bash array**, with no string quoting, and runs
  `tmux -S "$socket" display-menu -c "$client" -t "$pane" -T " Fedora Desktop " -x C -y C "${items[@]}"`.
- Adding an item means a play drops one file. **No tmux.conf edit, no server reload.** The
  play that owns a feature ships that feature's item. For example, `play-claude-yolo.yml`
  ships the token item.
- How items know their context: the runner passes it explicitly as environment to the item,
  and does not rely on what happens to be inherited:
  - `TMUX_MENU_SOCKET`, from `#{socket_path}`. Its basename is `ccy` for ccy and cc sessions,
    and `default` for plain tmux.
  - `TMUX_MENU_SESSION`, from `#{session_name}`: `ccy-<project>[-N]`, `cc-<project>[-N]`, or
    a user-chosen name (`tmux-session.bash:73-83`).
  - `TMUX_MENU_PANE`, from `#{pane_id}`, and `TMUX_MENU_PANE_PID`, from `#{pane_pid}`.
  - `TMUX_MENU_PATH`, from `#{pane_current_path}` / `#{session_path}`, the project directory.
- **Container resolution: reuse what exists, add nothing new.** `ccy_session_containers`
  (`files/var/local/claude-yolo/lib/tmux-session.bash:183-260`) is a pure function. It walks
  from `podman run --name X` up to the owning pane over the `ps` table. `ccy-sessions` already
  wraps it as `session_containers`
  (`files/home/.local/bin/ccy-sessions:149-165`, sourcing `/var/local/claude-yolo/lib` at `:70,114-120`).
  A ccy item sources the same library and filters to `$TMUX_MENU_SESSION`. The container's
  current token name is already on the container as the label `ccy-token=...`.
- Rejected alternative: have the launcher stamp a tmux pane option such as
  `tmux set -p @ccy_container "$CONTAINER_NAME"` after `:2977`. It is cheaper at runtime, but
  it creates a second source of truth that can go stale (a container exits while the pane
  lives on). It also needs a CCY version bump on every change. The process-tree resolver is
  already the answer the code uses for the same question (comment at `tmux-session.bash:171-181`).
  Keep this in reserve only if resolving through `ps` turns out to be too slow.

### Item definition format (for option C)

Each item is one executable file, so the metadata and the command live together. Keys are
read by awk from leading comment lines:

```bash
#!/usr/bin/env bash
# tmux-menu-label: Swap Claude token for this session
# tmux-menu-key: t
# tmux-menu-when: ccy          # ccy | any   (ccy = only on the ccy server, session ccy-*)
# tmux-menu-mode: popup        # popup (gets a TTY; needed for any prompt/picker) | background
set -euo pipefail
...
```

- File name `NN-<id>` controls the order (`10-ccy-token-swap`). `<id>` is validated as
  `^[a-z0-9-]+$`, so it can be embedded in a tmux command string without escaping.
- The generated menu item command is
  `display-popup -EE -T " <label> " -w 80% -h 60% -d '<path>' '<runner> run <id> <socket> <session> <pane>'`.
  Every interpolated value is either validated against a safe character set (`id`) or comes
  from tmux formats with known shapes (`%12`, `/dev/pts/N`, socket paths). The runner
  validates each one and rejects anything else.
- Alternative format: a data-only file (`label=`/`key=`/`exec=`) pointing at a separate
  script. It doubles the file count for no gain, so a self-describing executable is simpler.
  Prefer it only if items must ever be non-bash.

## 4. Constraints and pitfalls

### `display-menu` sizing [tmux man page / tmux source]

A menu is drawn only if the client has at least item-count + 2 rows and label width + 4
columns. **If not, tmux shows nothing and reports no error.** On a small terminal, F12 then
looks broken with no explanation.

- Keep the top level small. Today it is 6 items plus a separator; adding one submenu entry
  makes 7 entries, which is about 10 rows.
- The runner should compare the item count to `#{client_height}` / `#{client_width}` and,
  when the menu will not fit, say so with `display-message -d 0` ("terminal too small for the
  menu: needs N rows").
- Popups take percentages (`-w 80% -h 60%`), so they always fit.

### Labels

- Labels are expanded as formats: a literal `#` must be written `##`.
- A label beginning with `-` shows as a disabled item.
- An empty label `""` is a separator.
- Non-ASCII (`▸`) needs a UTF-8 client. That is the normal case on Fedora, but on a
  `LANG=C` SSH session it shows as `_`. Use ASCII `>` if that matters.
- The runner should reject labels containing `#` or a leading `-`, or escape them.

### Keys

- Each key is a single character or a tmux key name. When two items share a key, only the
  first one fires and nothing warns you.
- The keys in use at the top level are `n r s d k`, plus the new submenu key (`f` is free).
- Inside the submenu, the runner must **fail loudly on a duplicate key across drop-ins**:
  error out, do not pick one.
- `q` and `Escape` close a menu, so neither can be an item key.

### Quoting layers

`tmux.conf` parser, then the tmux command parser (again for each item command), then
`run-shell` / `display-popup` hand the string to `/bin/sh -c`. That is three layers.

- The existing entries already need `\"...'%%'...\"` nesting (`tmux.conf:25-26`).
- Option C keeps the conf side to one fixed string, and builds everything else in bash arrays
  passed as separate argv elements to `tmux display-menu`. Only the item command string is
  parsed again, and it contains only validated tokens.
- Never put free text from a drop-in into a command string.

### Format expansion in item commands

tmux expands formats in an item's command when the item is chosen [tmux man page]. Any
literal `#` in a generated command must be doubled. A command made only of safe tokens
avoids this.

### Running with no TTY

- `run-shell` has no terminal, so an item that prompts (for example an fzf token picker like
  `ccy_tmux_pick`, `tmux-session.bash:397-430`) **must** run in `display-popup`.
- `display-popup` needs tmux 3.2 or later. 3.7c is recorded, so that is fine.
- The `-E` form closes the popup when the command exits. **`-EE` closes it only when the
  command succeeds**, so on failure the output stays on screen until a key is pressed
  [tmux man page]. That gives fail-loud behaviour from tmux itself.

### Making failures visible (fail-fast)

- With `run-shell -b`, a non-zero exit shows `'cmd' returned N` in the message line. That
  line shows even with `status off`, but only for `display-time`, which defaults to 750 ms.
  That is too quick to count as loud.
- So the runner itself must report errors with `tmux display-message -d 0 -c "$client" "<what failed and what to do>"`.
  `-d 0` keeps the message until a key is pressed (tmux 3.2 or later).
- Errors here include: a malformed drop-in, a duplicate key, a menu that does not fit, a
  missing library, and an item asked for in the wrong context.
- The runner must also exit non-zero.
- Item scripts run in a `-EE` popup. They print the error to stderr and exit non-zero, and
  the popup stays open.
- No skip-and-warn anywhere. An invalid drop-in breaks the whole submenu with a named error
  rather than being quietly left out. Silently dropping it is exactly the "skip and warn"
  pattern `CLAUDE.md` prohibits.

### Checking at deploy time as well as press time

The runner should have a `check` subcommand that the play runs after copying the drop-ins.
A bad item then fails `ansible-playbook`, matching the existing config-check task
(`play-tmux-sessions.yml:28-37`), instead of surfacing the next time someone presses F12.

### Scope of the menu

`/etc/tmux.conf` is shared by every server, so the submenu appears in plain tmux too. Items
marked `when: ccy` are filtered out unless the socket basename is `ccy` and the session name
starts with `ccy-`. The runner checks again in `run`, because the context could have changed
between opening the menu and choosing the item.

### The top-level "New session" item on the ccy server

Out of scope, but noted: it creates a non-ccy shell session on the ccy socket. This is
existing behaviour and unrelated to the extension.

### Clashing with Claude Code

F12 is a root-table binding (`bind -n`), so tmux consumes it and Claude Code never sees it.
The submenu adds no new root-table keys.

## 5. Recommended design and the IaC changes

**Option C: one static "Fedora Desktop" submenu entry in F12, and a host-side runner that
builds the submenu at press time from `/etc/tmux-menu.d/` drop-ins.** Each item is one
self-describing executable owned by the play that owns its feature.

### Files

1. **`files/etc/tmux.conf`** (edit). Add one item to the F12 `display-menu`, for example
   after the separator:
   `"Fedora Desktop >" f "run-shell -b '/usr/local/bin/tmux-desktop-menu menu #{q:socket_path} #{q:client_name} #{q:pane_id}'"`.
   Update the header comment so it says that items come from `/etc/tmux-menu.d/`.

2. **`files/usr/local/bin/tmux-desktop-menu`** (new; name to be agreed, plain words rather
   than jargon). Bash with `set -euo pipefail`. Subcommands:

   - `menu <socket> <client> <pane>`: parse the drop-ins, validate, filter by `when`, check
     the menu fits, then `exec tmux -S ... display-menu ...`.
   - `run <id> <socket> <session> <pane>`: re-validate the context, export the
     `TMUX_MENU_*` environment, then `exec` the item.
   - `check`: validate every drop-in. Used by the play.
   - `--help`.

   It is non-interactive (tmux calls it), so it fails fast. Diagnostics go to stderr AND to
   `display-message -d 0` (`CLAUDE/StderrHygiene.md`). Keep the parsing and assembly in a
   pure function so it can be unit-tested.

3. **`/etc/tmux-menu.d/`**: a directory created by `play-tmux-sessions.yml`. Drop-ins come
   from the owning plays:

   - **First item:** `files/etc/tmux-menu.d/10-ccy-token-swap`, deployed by
     `playbooks/imports/play-claude-yolo.yml`, since the token feature belongs to ccy.
     `when: ccy`, `mode: popup`. It sources `/var/local/claude-yolo/lib/{common-pure,tmux-session,token-management}.bash`,
     resolves the container through `ccy_session_containers` filtered to `$TMUX_MENU_SESSION`,
     and fails loudly if there is none ("this session has no running container").
     Then it does whatever swap mechanism the other research settles on. Note that the token
     reaches the container as `-e CLAUDE_CODE_OAUTH_TOKEN` at `podman run` (`claude-yolo:~3246`),
     so an env-only swap is not possible in place. That is the other agent's question.
     As an interactive script it must follow `CLAUDE/InteractiveScripts.md`: bounded retry,
     a clean exit on EOF, `--help`, a safe default on confirmation, and secrets never put in argv.
   - Should this drop-in live under `files/var/local/claude-yolo/`? If it does, its changes
     need a CCY version bump. Placing it under `files/etc/tmux-menu.d/` avoids the bump while
     keeping ccy ownership through the deploying play. The launcher needs no change either way.

4. **`playbooks/imports/play-tmux-sessions.yml`** (edit):

   - create `/etc/tmux-menu.d` (`root:root 0755`);
   - copy the runner to `/usr/local/bin/` with mode `0755`;
   - after the existing config check, run `tmux-desktop-menu check` with `changed_when: false`,
     so a bad drop-in fails the play.
   - Optionally, a `become: false` task that re-sources `/etc/tmux.conf` into the user's
     running servers. This is a probe-then-act on the `ccy` and default sockets: act only when
     the server answers, fail on any other error. It applies the one-time F12 change without
     anyone restarting sessions. Whether a playbook should reach into live sessions is the
     owner's call. Without the task, `docs/` must say "run `tmux -L ccy source-file /etc/tmux.conf`
     or restart sessions once" — and under strict IaC it would have to be the task instead.

5. **`playbooks/imports/play-claude-yolo.yml`** (edit): copy its drop-in into `/etc/tmux-menu.d/`.
   It depends on the directory from `play-tmux-sessions.yml`. `playbook-main.yml:42` imports
   tmux, so check the order against where play-claude-yolo is imported, and have the ccy play
   fail with "run play-tmux-sessions.yml" if the directory is missing.

6. **Tests**: `scripts/test-tmux-desktop-menu.bash`, covering the parse and validate function
   with fixtures (duplicate key, bad id, `#` in a label, missing label, `when` filtering).
   Wire it into `scripts/qa-all.bash` the same way the other `test-*.bash` suites are
   (pattern at `scripts/qa-all.bash:317,334,351,373`).

7. **Docs**:

   - `docs/tmux-sessions.md`: add a row for the new item to the table at `:16-22`, plus a
     short "Adding a menu item" section pointing at the drop-in format.
   - `docs/ccy.md`: describe the token-swap item.
   - `docs/playbooks.md:641`: update.
   - Changes under `files/var/local/claude-yolo/` also need a `docs/ccy-changelog.md` entry.

8. **Plan**: this is plan-sized (a new mechanism plus its first consumer). Scaffold it with
   `CLAUDE/Plan/mkplan.bash`. The `qa-reviewer` agent is the final step.

### Open questions for the owner

- The runner's name and the submenu label. The repo prefers plain words.
- Whether the play should re-source running tmux servers (see 4 above).
- Whether `when: ccy` items should also appear for `cc-` sessions on the same socket. The
  token swap probably should not, since `cc` runs Claude on the host.
