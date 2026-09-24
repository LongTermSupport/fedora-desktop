# Research: cwd-weighted Ctrl+R ranking, and the security of each option

Scope: Fedora 44, bash 5.3, many concurrent terminals and tmux panes. It follows
[RESEARCH-optimal-config.md](RESEARCH-optimal-config.md) and [PROPOSAL.md](PROPOSAL.md).
The owner has rejected fzf-over-in-memory-history. Two hard requirements:

- **A: weighting, not filtering.** Ctrl+R always searches all history (every directory,
  every terminal). Commands previously run in the current directory rank higher, then
  commands run in the current git repository, then everything else. A mode that *hides*
  other directories is not acceptable as the default.
- **B: no new security holes.**

Method: the source was read at two points, not just the docs:

- **Atuin `v18.12.1`**, the tag Fedora 44 ships (`atuin-18.12.1` in updates). Its source RPM
  was also unpacked to read the spec.
- **Atuin upstream `main`** at the time of writing (latest release **18.23.0**).

Paths below are relative to <https://github.com/atuinsh/atuin> at the stated tag. Facts
marked **(host)** were read on a Fedora 44 machine.

---

## 1. Can Atuin do requirement A?

**Short answer:**

- **On Fedora's 18.12.1: partially.** One search mode, `skim`, does weight by directory
  distance while in global mode.
- **On upstream ≥ 18.19.0: no.** That mode was removed. Every remaining mode ranks by
  match quality, recency and frequency only. The current directory and repository can
  only *filter*.

### 1.1 What each search mode ranks by

| `search_mode`  | Present in         | Ranking in global filter mode                                                                                                                                                                                                                                                               | Uses cwd for ranking?  |
| -------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| `prefix`       | all                | SQL `ORDER BY timestamp DESC`, deduplicated to the newest row per command. Source: `crates/atuin-client/src/database.rs`, `search()`                                                                                                                                                        | No                     |
| `fulltext`     | all                | Same SQL ordering                                                                                                                                                                                                                                                                           | No                     |
| `fuzzy`        | all (default)      | Same SQL (limit 200), then reordered by fuzzy match (`ordering::reorder_fuzzy`)                                                                                                                                                                                                             | No                     |
| `skim`         | **≤ 18.18.x only** | Loads all history in memory, aggregated per command, and scores `fuzzy_score × log2(count+8) / log2(path_dist+8) / log2(age_s) / log2(match_pos+16)`. Source: `crates/atuin/src/command/client/search/engines/skim.rs` at `v18.12.1`                                                        | **Yes: path distance** |
| `daemon-fuzzy` | ≥ 18.13.0          | In-memory index in the daemon. Before 18.19.0 it used nucleo plus frecency. From **18.19.0** it is "rank by match quality with frecency tiebreak" (#3782). Frecency is **global**: recency buckets plus `ln(count)×20` (`crates/atuin-daemon/src/search/index.rs`, `FrecencyData::compute`) | **No**                 |

- `skim` was **removed in 18.19.0** ("Remove the 'skim' engine", PR #3825, merged at the end
  of July 2026). A config that still says `skim` now falls back to `fuzzy` and shows a
  warning in the TUI (`crates/atuin-client/src/settings.rs`, `RequestedSearchMode::Skim`
  maps to `SearchMode::Fuzzy`).
- The daemon index keeps per-command directory sets. It uses them only for
  `IndexFilterMode::Directory` / `Workspace` **filtering** (`has_invocation_in_dir`,
  `has_invocation_in_workspace`). The `Score` struct holds only `fuzzy_score`, `frecency`
  and `index`.
- `smart_sort` (default `false`; undocumented, issue #2167) re-sorts results by
  prefix/substring match and recency only (`crates/atuin-history/src/sort.rs`). Its source
  comment says "Later on, we can pass in context and do some boosts there too". That has
  not been implemented.
- The frecency multipliers (`search.recency_score_multiplier`,
  `frequency_score_multiplier`, `frecency_score_multiplier`, added in 18.13.0, #3235) tune
  the global frecency score only. There is no directory or workspace multiplier.

### 1.2 How good the 18.12.1 `skim` weighting actually is

`path_dist(entry_cwd, current_cwd)` counts the steps up to a common ancestor, then the steps
down. The resulting factor `log2(dist+8)` is 3.0 at distance 0, about 3.6 at distance 4,
about 4.2 at distance 10, and about 4.8 at distance 20.

Consequences, all read from the `v18.12.1` source:

- **Same directory beats an unrelated one by at most about 1.4×.** Recency divides by
  `log2(age in seconds)`, which is about 16.4 for one day and 21.3 for one month.
  - A same-directory command from a month ago roughly ties with an unrelated command from a
    day ago.
  - The weighting is real but **mild**.
- **No explicit git-workspace term.** Commands elsewhere in the same repository are
  "closer" than commands in unrelated trees, so the repository is favoured *approximately*.
  A sibling repository under the same parent directory scores the same as a distant
  subdirectory of the current one.
- **An empty query gets no weighting.** `SkimMatcherV2` returns score 0 for an empty
  pattern (`fuzzy-matcher` `src/skim.rs`), so every candidate scores 0. The list is then
  pure recency (the `all_with_count` SQL order).
  - The "cd into a project and press Ctrl+R" case therefore does **not** surface the
    directory's commands until you type at least one character.
- **The aggregation corrupts directory distance for multi-directory commands.**
  - `all_with_count()` groups by `command, exit` and builds `group_concat(cwd, ':')`
    (`crates/atuin-client/src/database.rs` at `v18.12.1`).
  - `path_dist` is then run on the joined string as though it were one path. A command run
    in several directories gets a distorted distance.
  - The directory **filter** splits on `:` correctly. The **ranking** does not.
- **Imported history has no directory.** `atuin import bash` sets `cwd = "unknown"`
  (`crates/atuin-client/src/history/builder.rs`, `HistoryImported`). Weighting only builds
  up for commands recorded after Atuin is enabled.
- **Performance.** Every Ctrl+R loads the whole table into memory. Upstream removed the
  engine partly for ">300ms of stutter" (PR #3825 discussion). Not measured here.
- **Upgrade cliff.** When Fedora moves atuin to ≥ 18.19, `search_mode = "skim"` silently
  becomes `fuzzy`: a TUI warning only, no error. Weighting would disappear without any
  failure. Under the fail-fast rule, any adoption of `skim` needs an IaC assertion that the
  installed version is `< 18.19.0`, so that an upgrade fails loudly.

### 1.3 Filter modes, workspaces, cycling order

| Setting                            | Values / default                                                                                                                              | Since                                                   |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| `filter_mode`                      | `global`, `host`, `session`, `directory`, `workspace`, `session-preload`. Default unset: first usable entry of `search.filters`               | `session-preload` 18.9.0                                |
| `search.filters`                   | Cycle order for Ctrl+R **inside** the TUI. Default `["global","host","session","workspace","directory","session-preload"]` (18.12.1 and main) | 18.4.0 (#2430)                                          |
| `workspaces`                       | Enables the `workspace` mode when inside a git repo. Default **`false` in 18.12.1**, `true` on main (the online docs still say `false`)       | ≤ 17.0.0 (key renamed to `workspaces` in 17.0.0, #1174) |
| `filter_mode_shell_up_key_binding` | Filter used when opened from the Up key                                                                                                       | long-standing                                           |
| workspace → git worktree root      | Worktrees resolve to the main repository                                                                                                      | 18.14.0 (#3366)                                         |

`default_filter_mode()` (`crates/atuin-client/src/settings.rs`) skips `workspace` unless
`workspaces = true` and the cwd is in a git repo.

**Closest pure-filter configuration** (a fallback, which does not meet A): start in
`global`, set `search.filters = ["global", "workspace", "directory"]`. Inside the TUI, one
Ctrl+R then narrows to the repository and a second narrows to the directory. This is
navigation, not ranking.

### 1.4 Upstream requests

No issue or PR asks specifically for **cwd-weighted ranking in global mode**, and nothing is
in progress. The nearest ones:

- **#3037** (open, Dec 2025), "Session-boosted ranking in global filter mode". The same
  shape as A, but for session. No comments.
- **#3534** (open, Jun 2026), `atuin search --filter-modes directory,session,global`. An
  ordered fallback, not a blend.
- **#1549** (open, Jan 2024), "Neural-network based ranking (à la McFly)". A commenter
  explicitly asks for directory to be used "as input into sorting and not an exclusion".
- **#2481** (open), "Filter based on context".
- **#2631** (closed): frecency, answered by `daemon-fuzzy` in 18.13. Global only.

### 1.5 Is there a small, upstream-supported hook?

There is no scoring plugin or hook API. There **is** a stable, documented CLI that exposes
everything a ranker needs, without reading Atuin's internal schema:

```
atuin search --filter-mode global --include-duplicates --print0 \
             --format '{directory}\t{exit}\t{command}'
```

- All flags exist in 18.12.1 (`crates/atuin/src/command/client/search.rs`).
- Output is newest-first (`ORDER BY timestamp DESC`), so the position of a command's first
  occurrence *is* its recency rank.
- A short `awk` pass can score each distinct command, for example:
  - `+big` if any run has `directory == $PWD`;
  - `+medium` if any run is under `$(git rev-parse --show-toplevel)`;
  - `+small × log(count)`;
  - `−penalty` if every run failed;
  - recency as the tiebreak.
- It then pipes to the **already installed** `fzf` with `--tiebreak=index` (equal match
  scores keep the weighted order). Use `--no-sort` instead if the weighted order should win
  outright over match quality.
- Bind it with `bind -x` to Ctrl+R. Keep Atuin's own TUI on another key, or drop it.

This is the only route that gives **exactly** A (directory > repository > everything,
over all history, including an empty query) and does not depend on a removed engine. The
cost is a small piece of shell this repository owns and tests. It has not been prototyped
or timed here.

---

## 2. McFly

Source: <https://github.com/cantino/mcfly> (latest release **v0.9.4**, Dec 2025).

- **Ranking (source `src/history/history.rs`, `build_cache_table`; `src/node.rs`).** A small
  trained network combines per-command features over **all** history. The default
  `MCFLY_RESULTS_FILTER` is `GLOBAL`; `CURRENT_DIRECTORY` is opt-in. The features:
  - `dir_factor`: the share of runs in **exactly** the current directory.
  - `selected_dir_factor`: the share of past McFly selections made in this directory.
  - `exit_factor`, plus `recent_failure_factor`.
  - `age_factor` (recency) and `occurrences_factor` (frequency).
  - `selected_occurrences_factor` (selection feedback).
  - `overlap_factor` / `immediate_overlap_factor`: which commands usually follow the last
    one to three commands.
  - Ranking also applies to an empty query, which is the main point.
- **Gaps against A:**
  - **No git-workspace term.** The directory match is exact equality, not a prefix.
  - **No built-in secrets filter.** `MCFLY_IGNORE_PATTERN` (user regex) was merged in
    Aug 2026 and is **not in a release**.
  - A leading space is ignored (`is_ignored_command`).
- **Maintenance:**
  - The README says "Seeking co-maintainers: I don't have much time to maintain this
    project these days."
  - Commits continue (Aug 2026), but releases are sparse: 0.9.3 in Feb 2025, 0.9.4 in
    Dec 2025. There are 135 open issues.
- **Fedora:**
  - **Not packaged** (host `dnf repoquery` finds nothing).
  - Release assets are tarballs **without published checksums**, so IaC would have to pin
    its own sha256. The alternative is building with `cargo`.
- **TIOCSTI:** since 0.9.0 bash no longer uses TIOCSTI by default (CHANGELOG). It binds
  Ctrl+R to a macro of two dummy key sequences (`\C-x1` runs `mcfly search -o <file>` via
  `bind -x`; `\C-x2` is bound to `accept-line` or nothing). It therefore works with
  `dev.tty.legacy_tiocsti = 0` (host). `MCFLY_BASH_USE_TIOCSTI=1` would re-enable the old
  path and fail on this host.
- **Bash integration (`mcfly.bash`).** A `PROMPT_COMMAND` array element, with no DEBUG
  trap. On every prompt it:
  - `history -a` into a per-session temp file `${TMPDIR:-/tmp}/mcfly.XXXXXXXX`, seeded
    with the last 100 lines of `$HISTFILE`;
  - runs `mcfly add`, which reads the command from that file (not argv) and **itself
    appends to `$HISTFILE`**;
  - then runs `history -cr <tempfile>`.
- **Conflicts with this plan's P1–P3:**
  - McFly owns `$HISTFILE` writes, so P1's `history -a` would double-write.
  - It writes bash lines **without `#epoch` timestamps** (`HistoryCommand` `Display` for
    `HistoryFormat::Bash`), which breaks P2 (`HISTTIMEFORMAT` + `lithist` multi-line
    entries).
  - Each prompt replaces the in-memory list with the 100-line temp file, so P3's unlimited
    `HISTSIZE` no longer means anything in-shell.
- **Security notes:**
  - It has no network dependencies (`Cargo.toml`).
  - Its data is `~/.local/share/mcfly/history.db` plus a training cache under `~/.cache`,
    created with the process umask.
  - Per-session temp files hold recent commands and are **not removed** on exit. `mktemp`
    creates them 0600, and `/tmp` is cleared at boot.
  - The output file uses `mktemp --dry-run`, which is a create race, mitigated by
    `fs.protected_symlinks`. Setting `TMPDIR` to `$XDG_RUNTIME_DIR` for McFly removes both
    concerns.

**Verdict:** McFly's ranking is the closest off-the-shelf match to A. Adopting it would
mean an unpackaged, thinly maintained binary that takes over the plain history file and
undoes P1–P3.

---

## 3. Other tools

| Tool                                                | cwd-weighted over all history?                                                | Status                                                 | Fedora    | Security note                                                                                                                                                     |
| --------------------------------------------------- | ----------------------------------------------------------------------------- | ------------------------------------------------------ | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **RESH** <https://github.com/curusarn/resh>         | Yes: "relevant results based on current directory, git repo, and exit status" | **Abandoned** (last push and release v3.0.2, May 2023) | No        | Runs a daemon on **unauthenticated `localhost:2627` HTTP** (`internal/cfg/cfg.go`, `cmd/daemon/run-server.go`): any local UID can talk to it. **Reject**          |
| **hiSHtory** <https://github.com/ddworken/hishtory> | No documented weighting; table search with `cwd:`-style filters               | Maintained (v0.335, Feb 2025; pushes 2026)             | No        | **Syncs to a hosted backend by default** (E2E-encrypted; `--offline` / `hishtory syncing disable`); AI queries routed via its servers unless disabled. **Reject** |
| **hstr** <https://github.com/dvorka/hstr>           | No (frequency/recency "ranking" view; no cwd)                                 | Maintained (v3.2, Feb 2026)                            | Yes (3.1) | Plain `$HISTFILE`; TIOCSTI caveat covered in the earlier research                                                                                                 |
| **ble.sh** <https://github.com/akinomyoga/ble.sh>   | Not a ranker. It is the best preexec host for Atuin (no DEBUG trap)           | Active on master; last tagged v0.4.0-devel3 (2023)     | No        | Replaces readline and overrides builtins; large blast radius                                                                                                      |

No other maintained, packaged tool was found that weights by directory over full bash
history.

---

## 4. Atuin security (18.12.1 unless stated)

### 4.1 Data at rest

- **Location:** `~/.local/share/atuin/`:
  - `history.db` (**plaintext** SQLite with WAL: command, cwd, exit, duration, hostname,
    user, session, time);
  - `records.db` (the record store used for sync);
  - `meta.db`, `key`, `host_id`.
  - Config goes in `~/.config/atuin/config.toml`.
- **Modes:** the `atuin` binary sets **umask 077** at startup (`crates/atuin/src/command/mod.rs`,
  "Set umask 077", PR #1554, released in **18.0.0**). Everything it creates is therefore
  0600 files and 0700 directories. This fixed issue #1250 (key and DB world-readable).
  - Files and directories created by **anyone else** (IaC) do not benefit. IaC must set
    0700 on `~/.local/share/atuin` and `~/.config/atuin`, and 0600 on `config.toml`,
    explicitly.
- **Encryption key:** `~/.local/share/atuin/key` (`key_path`), created 0600.
  - Upstream `main` also chmods it to 0600 explicitly (`KEY_FILE_MODE`,
    `crates/atuin-common/src/encryption/paseto_v4.rs`).
  - It protects **sync** payloads. The local `history.db` is not encrypted, so without sync
    the key protects nothing extra. It must not be copied into this repository or vault
    unless sync is adopted.
- **More metadata than bash history.** Directory paths reveal project and customer names,
  and hostnames and timings are recorded. Treat `history.db` as at least as sensitive as
  `$HISTFILE`, and as more identifying.

### 4.2 Network: what can leave the machine

- **Update check: on by default in Fedora's build.**
  - `update_check` defaults to `cfg!(feature = "check-update")`.
  - Fedora builds with `%cargo_build -a` (all features; `atuin.spec`), so the feature is
    compiled in.
  - When the interactive search opens (`interactive.rs`, `needs_update()`), Atuin sends
    `GET https://api.atuin.sh` with an Atuin user-agent, **at most once per hour**
    (`needs_update_check`, `api_client::latest_version`).
  - No history is sent, but it leaks IP, version and usage timing. **Set
    `update_check = false`.**
- **Sync: off unless logged in.** `should_sync()` returns false unless `auto_sync` **and** a
  stored session exist (`settings.rs`). Nothing is uploaded without `atuin register` /
  `atuin login`.
  - `sync_address` defaults to the hosted `https://api.atuin.sh`.
  - Belt and braces: `auto_sync = false`, and optionally point `sync_address` at an
    unroutable local address so that a mistaken `atuin login` cannot reach the hosted
    service.
- **Telemetry:** none found in the client source.
- **Daemon:**
  - **Off by default** (`daemon.enabled = false`, `autostart = false`).
  - On Unix it listens only on a **Unix socket**:
    - 18.12.1: `$XDG_RUNTIME_DIR/atuin.sock`, and `$XDG_RUNTIME_DIR` is 0700 per user;
    - main: `$TMPDIR/atuin-$UID/atuin.sock`, in a directory it creates 0700 and refuses to
      use if group- or other-accessible (`crates/atuin-common/src/os/unix.rs`).
  - `tcp_port = 8889` is used **only on non-Unix** systems.
  - No listening port on Linux. Keep the daemon off: it is only needed for `daemon-fuzzy`,
    which does not help A.
- **Newer upstream features to keep off after an upgrade** (not in 18.12.1):
  - AI features. The `ai` crate is in the default features on main. Settings `[ai]`;
    `ai.send_cwd` defaults to `false`.
  - Output capture. `output.enabled` defaults to `false` (`settings/output.rs`), and
    captured output can sync (`output.sync`).
  - Log files (`logs.enabled` defaults to `true` on main).
  - Re-audit the config on every major Fedora bump.

### 4.3 What gets recorded, and the filters

- `History::should_save` (`crates/atuin-client/src/history.rs`) refuses to record a command
  if any of these is true:
  - it starts with a space;
  - it is empty;
  - it matches `history_filter`;
  - its cwd matches `cwd_filter`;
  - `secrets_filter` is on **and** it matches the built-in patterns.
- **`secrets_filter` is on by default.** The patterns are in
  `crates/atuin-client/src/secrets.rs`:
  - AWS key IDs, and `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN` env vars;
  - `AZURE_*_KEY` and `GOOGLE_SERVICE_ACCOUNT_KEY`;
  - `atuin login`;
  - GitHub tokens: old and new PATs, and `gho_`, `ghu_`, `ghs_`, `ghr_`, `v1.` tokens;
  - GitLab PATs;
  - Slack bot and user tokens and webhooks;
  - Stripe, npm, Netlify and Pulumi tokens.
  - History of the list: extended in 18.0.0, 18.4.0 and 18.9.0.
  - It does **not** cover generic passwords, `--password=…`, bearer headers, URLs with
    embedded credentials, private keys pasted inline, or vault passwords. Add those via
    `history_filter`.
- **`history_filter` / `cwd_filter`:**
  - Both are unanchored regex sets, empty by default.
  - They apply at record time and are **not retroactive**.
  - `atuin history prune` deletes stored entries that now fail `should_save`.
- **`store_failed`** (default `true`): failed commands are kept, which is useful for
  ranking.
- **argv exposure:**
  - The bash hook runs `atuin history start -- "$1"` (`crates/atuin/src/shell/atuin.bash`;
    still the case on main). **Every command line is passed as argv** to a short-lived
    process.
  - `/proc` is mounted without `hidepid` (host), so other local UIDs can read it while it
    runs.
  - External commands already expose their argv. The new exposure is shell-only lines
    such as `export TOKEN=…`, which normally never appear in any argv.
  - The window is short but real. Leading-space commands are passed too: `should_save`
    drops them *after* the process has started. Mitigation: `hidepid=2` on `/proc` is
    system-wide and out of scope here. **Record this as an accepted residual risk, or
    reject Atuin on it.**

### 4.4 bash-preexec, ignorespace, DEBUG trap

- **18.12.1 does not bundle a preexec backend.** The bundling arrived in 18.18.0 (#3650),
  so Fedora's packaged `bash-preexec` **0.6.0** must be sourced first.

- **0.6.0 uses a DEBUG trap.** Upstream **0.7.0** (Aug 2026) switches to a `PS0`
  command-substitution hook on bash ≥ 5.3, with no DEBUG trap (`__bp_hook_preexec_into_ps0`).
  Fedora has 0.6.0.

- **The ignorespace hole.** bash-preexec's `__bp_adjust_histcontrol` **removes `ignorespace`
  and turns `ignoreboth` into `ignoredups`, and exports the result.** It does this in 0.6.0
  and still in 0.7.0; the upstream issue is rcaloras/bash-preexec#115, still open.

  - Space-prefixed commands then **do go into `$HISTFILE`**. Atuin still refuses them
    (`should_save`).
  - This silently breaks P3's escape hatch. It is exactly the kind of new hole
    requirement B forbids, so it needs a fix, not a note.

- **Fix, owned by this repository.** In the P1 hook, before appending, drop the newest
  entry if it starts with a space:

  ```bash
  __history_append() {
      local last
      last=$(HISTTIMEFORMAT='' builtin history 1)
      last=${last#*[[:digit:]][* ] }
      [[ $last == ' '* ]] && builtin history -d -1   # bash ≥ 5.0
      builtin history -a
  }
  ```

  - This runs after the command, at the next prompt. bash-preexec has already read the
    command, and nothing has been written yet.
  - Residual risk: a shell killed by SIGHUP **while** a space-prefixed command is still
    running saves its in-memory list on exit, including that line.

- **`HISTIGNORE` pollutes Atuin.** The preexec hook reads the command from `history 1`. A
  command excluded by `HISTIGNORE` (`ls`, `bg`, `fg`, `exit` in the current config) never
  enters the list, so `history 1` returns the **previous** command.

  - Atuin then records that previous command a second time, with the new cwd and exit
    status.
  - This is inferred from the source of both projects, not documented upstream.
  - It corrupts directory statistics, which are exactly what A relies on. With Atuin,
    empty `HISTIGNORE`, or accept the noise.

- **DEBUG-trap risks (0.6.0):**

  - The trap runs before every simple command. bash-preexec preserves a pre-existing DEBUG
    trap as a preexec function.
  - It must control `PROMPT_COMMAND`: it prepends `__bp_precmd_invoke_cmd` to element 0
    and appends `__bp_interactive_mode`.
  - With `extdebug` set, a failing preexec would **block** the command.
  - Subshells, function definitions and some compound commands do not fire preexec.
  - No DEBUG trap is set by Fedora's `vte.sh` or `80-systemd-osc-context.sh` (host). Those
    use `PROMPT_COMMAND` array elements and `PS0`.
  - These are correctness risks rather than privilege risks.

- **Do not install `atuin-all-users`.** It ships `/etc/profile.d/atuin.sh` sourcing a
  **static** `atuin init bash` generated at build time (`atuin.sh.in`). That:

  - enables Atuin for **every** user, including root;
  - cannot take `--disable-up-arrow`;
  - does not load bash-preexec.

  Source `atuin init bash --disable-up-arrow` from the repository's own profile snippet
  instead, after bash-preexec.

- **`enter_accept`:**

  - The built-in default is `false`, but the **config file Atuin writes on first run sets
    `enter_accept = true`** (`crates/atuin-client/config.toml`; the source comment says the
    mismatch is "intentional"). Enter then runs the selected command immediately.
  - Ship `config.toml` from IaC **before** first run, with `enter_accept = false`, so a
    recalled command is always reviewed.

- **No TIOCSTI use.** The bash widget sets `READLINE_LINE` via `bind -x`.

### 4.5 root and sudo

- Atuin keys everything off `$HOME` (data, config) and the environment.
- With sudo's default `env_reset`, `sudo -i` / `sudo -s` get root's `HOME`, so a root shell
  would use root's own Atuin store, if root had Atuin enabled at all.
- `sudo -E`, `--preserve-env=HOME` and `su -m` / `su -p` keep the caller's `HOME`. A root
  shell with Atuin loaded would then write into the user's `~/.local/share/atuin` and could
  create **root-owned** WAL or SHM files there, breaking the user's Atuin.
- **Recommendation:** do not enable Atuin for root. Keep root on plain bash history (§5).
  Guard the Atuin init with `[[ $EUID -ne 0 ]]`.

### 4.6 `atuin import`

- `atuin import bash` reads `$HISTFILE` **from the environment** (`import/mod.rs`,
  `get_histpath`), otherwise `~/.bash_history`.
  - `HISTFILE` is a shell variable and is normally **not exported**. After P4 the import
    must be run as `HISTFILE=~/.local/state/bash/history atuin import bash`, or it will
    import the old abandoned file.
- It copies every line as a command, with `cwd = "unknown"`, exit `-1`, and timestamps from
  `#epoch` lines where present.
- It does **not** apply `should_save`. Secrets, space-prefixed lines and filtered commands
  in the old file are all imported. Run `atuin history prune` immediately after import, and
  add the extra `history_filter` patterns first.

### 4.7 CVEs and advisories

- **Advisories:** GitHub's advisory database has none for `atuin`, `atuin-client` or
  `atuin-server` (queried by crate). The repository has no published security advisories.
- **Fixed defect:** #1250, world-readable key and DB, fixed by umask 077 in 18.0.0.
- **Dependency rebuilds:** Fedora's changelog records dependency-driven rebuilds for
  RUSTSEC-2026-0007/0008/0009, CVE-2026-25537 and CVE-2024-12224 (idna).
- **No audit:** there is no independent audit (#2484, open). The security contact is in
  `SECURITY.md`.

### 4.8 Hardened `~/.config/atuin/config.toml` (18.12.x)

IaC-deployed, file 0600, directory 0700, placed before first run:

```toml
## Nothing leaves the machine
update_check = false               # else Ctrl+R GETs https://api.atuin.sh (hourly max)
auto_sync = false                  # sync already needs a login; this makes it explicit
# sync_address = "http://127.0.0.1:1"   # optional: a mistaken `atuin login` goes nowhere

## Recording
secrets_filter = true              # default, stated for clarity
store_failed = true                # failures are ranking signal
history_filter = [
  '(?i)\b[A-Z0-9_]*(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY)[A-Z0-9_]*=',
  '(?i)--password[= ]',
  '(?i)authorization:\s*(bearer|basic)\s',
  '://[^/\s:@]+:[^/\s@]+@',        # credentials embedded in URLs
]
cwd_filter = [
  '/\.ssh(/|$)',
  '/\.gnupg(/|$)',
  '/\.password-store(/|$)',
]

## Search (weighting only exists in `skim`, only on < 18.19.0; see section 1.2)
search_mode = "skim"
filter_mode = "global"             # requirement A: never start filtered
workspaces = true
enter_accept = false               # always review a recalled command

[search]
filters = ["global", "workspace", "directory", "session"]

[daemon]
enabled = false                    # no socket, no background process
```

Init, in the repository's profile snippet: guard the Atuin block with
`[[ $EUID -ne 0 ]]`, source `/usr/share/bash-preexec/bash-preexec.sh` first, then
`eval "$(atuin init bash --disable-up-arrow)"`. Place it after the prompt framework and
after P1's hook is registered. Treat the exact paths and order as something to verify on
the host.

---

## 5. Security of the plain-bash changes (P1–P4)

- **Modes.** bash's history library creates the file with `open(…, 0600)`, and the umask
  can only tighten that. The directory must be created 0700 by IaC.
  - The **seed copy** from `~/.bash_history` is made by IaC, so it must set `mode: "0600"`
    explicitly rather than inherit.
  - Rewrites (`history -w`, fzf's Shift+Delete) replace the file with a new 0600 file.
- **Location.** The XDG Base Directory spec names "actions history (logs, history …)" as
  the purpose of `XDG_STATE_HOME`
  (<https://specifications.freedesktop.org/basedir-spec/latest/>). Check that home backups
  include `~/.local/state`. The history file is sensitive, so backups of it deserve the
  same protection as the rest of home.
- **Unlimited history.** Nothing ages out, so an accidental secret (a password typed at the
  wrong prompt) persists indefinitely. That is not new exposure, but it is longer
  retention. Mitigations:
  - the leading-space escape hatch, which must survive bash-preexec (§4.4);
  - `history -d N; history -w` to remove an entry.
- **Timestamps.** They add when-you-worked metadata. The file is local and 0600, so the risk
  is low.
- **Non-default `HISTFILE`.** This reduces risk: stray shells truncate the abandoned file.
  - A root shell that keeps the caller's `HOME` (`sudo -E`, `su -m`) would compute the
    **user's** path. Root would then append its commands to the user's file, or, if the
    directory were missing, create a root-owned file that the user's shells cannot write.
  - Guard the assignment so it only applies when the directory is owned by the current
    user, for example `[[ -O $dir ]]`. Otherwise warn on stderr and keep bash's default for
    that shell.
- **root.** Yes, set root up the same way (P1–P4), **without** Atuin:
  - durable, timestamped, unlimited, in `/root/.local/state/bash/history`, with the
    directory 0700 created by IaC. `/root` itself is not world-readable.
  - Root's history is the audit trail most worth keeping intact, and the truncation trap
    (`bash --norc` as root in a rescue context) is at least as likely there.
  - Keep the leading-space escape hatch for root too.
