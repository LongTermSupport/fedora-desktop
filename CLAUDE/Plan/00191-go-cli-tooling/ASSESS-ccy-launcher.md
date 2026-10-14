# Assessment: porting the ccy launcher (and cc) to Go

Plan 00191, Task 4.2. Research only: nothing in the repository was changed to produce this.
It is written for the owner to decide on. Structure and method follow
[SPEC-ccy-teams.md](SPEC-ccy-teams.md) and [SPEC-ccy-sessions.md](SPEC-ccy-sessions.md).

Abbreviations: L = launcher `files/var/local/claude-yolo/claude-yolo`; lib = `files/var/local/claude-yolo/lib/*.bash`;
CC = `files/var/local/claude-code/cc` plus `cc-teams.bash`; SR = `lib/session-registry.bash`; TS = `lib/tmux-session.bash`;
TM = `lib/token-management.bash`; SSH = `lib/ssh-handling.bash`; NM = `lib/network-management.bash`.
Line numbers are those of the tree at the time of writing and will drift.

## 1. Summary

- This is not the "about 4,000 lines" the plan names. The launcher is 4,002 lines, but it is a
  straight-line script that sources 11 libraries of 11,418 more lines. The program is **15,420
  lines of bash** (L + lib), plus CC (607 lines, which runs on the same libraries). Extrapolating the
  measured ratio of the two finished ports (about 2 lines of production Go per line of bash, and
  about as many lines of Go test again) gives **roughly 25,000 to 30,000 lines of production Go**
  for a complete port. It is a different order of work from ccy-teams (1,938 bash lines, 3,870 Go)
  or ccy-sessions (1,228 bash lines, 2,438 Go).
- **Nothing in the launcher runs inside the container.** The launcher, its libraries and CC run on the
  host. The container runs `entrypoint.sh` (bash, 756 lines), the supervisor (Python) and Claude Code.
  A Go launcher needs no Go in the image and no image rebuild. The contract between the two is
  a fixed list of environment variable names, mounts and labels (section 3).
- **The parity oracle is the weak point, not the code.** Of 34 `test-ccy-*` suites (12,651 lines),
  only **3** run the launcher as a program (`info-flags`, `token-usage-command`, `teams`), and those
  reach only argument validation and the early-exit modes. 11 suites extract text from the launcher
  with `awk`/`grep` (whole functions, `if` blocks, the `podman run` line) and 24 `source` the libraries and
  call functions. A Go binary passes none of the text-extracting suites and none of the sourcing
  suites. The `podman run` command line itself is covered only by `grep -c` on source text. Before any
  port of the main launch path there must be a **differential harness** (section 7).
- About **875 lines of the libraries are already dead** (the restore/manifest/transcript chain in SR
  and `ccy_known_prompts`, `ccy_tmux_take_over` and its two helpers in TS): the Go `ccy-sessions`
  replaced them and only tests call them. Deleting them is a safe, shippable first step that also
  removes bash oracles that no longer test anything real (section 5.3).
- Recommendation: do it, but as a **strangler at the same install path**: the Go binary is installed as
  `/var/local/claude-yolo/claude-yolo`, the bash launcher is renamed and `exec`ed for whatever Go
  has not taken yet. Every slice then ships with bash still working, session records and the shell
  alias keep their path, and each slice can be reverted by one play run. Take the standalone modes first
  (`--version`, `--help`, `--token-usage`, `--list-tokens`, `--export-token`, `--top`), then CC, then the main
  launch path in the order of section 8. Decide the open questions in section 9 first; D1, D2 and D4 shape
  everything else.

## 2. Size and structure

### 2.1 The launcher, by area (L, 4,002 lines)

The launcher defines 11 functions and is otherwise one linear script with 113 top-level shell
variables as shared state (the libraries set 182 more), 84 `exit 1`, 17 `exit 0`, 7 `exit 64`
paths, and 51 `read` prompts across L and lib. Line ranges are approximate.

| Lines (L) | Area                                                                                                                                                                                  | Approx. lines |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| 1-150     | Header, version, library loading, `CCY_HASH`, `--version`                                                                                                                             | 150           |
| 157-502   | `--help` text (a quoted heredoc)                                                                                                                                                      | 345           |
| 503-595   | Quick Launch file: `load_launch_config`, `save_launch_config`                                                                                                                         | 90            |
| 599-1018  | Flag parsing (about 50 flags, value-taking flags, `--`, `--teams` positions), validation, the "suspect flag" check for claude's own flags                                             | 420           |
| 1019-1093 | `--debug` layer chooser; directory structure                                                                                                                                          | 75            |
| 1103-1230 | Standalone modes (`--top`, `--prevent`, `--list-tokens`, `--export-token`, `--connect`, `--disconnect`, `--custom`, `--custom-docker`), tmux insulation call, restore/restart markers | 130           |
| 1230-1460 | SSH keys: Quick Launch accept, selection, validation, agent forward, mounts                                                                                                           | 230           |
| 1461-1840 | Token create/update/select, token validation recovery UI, rootless assertion                                                                                                          | 380           |
| 1842-2060 | Existing containers, rebuild, daily Claude Code auto-update (`auto_update_claude_code`, `update_claude_inplace`)                                                                      | 220           |
| 2059-2392 | Image check, project Dockerfile build, `repair_project_state_permissions`                                                                                                             | 330           |
| 2393-2565 | `cleanup` + EXIT trap, git config copy, commit signing, restore askpass, debug dump, mounts                                                                                           | 170           |
| 2567-3350 | **Network selection**, inline (`--network`, saved preference, auto-connect, cross-engine, compose offer)                                                                              | 780           |
| 3350-3600 | Summary box, Quick Launch save, container name, claude argument assembly, GUI/GPU flags                                                                                               | 250           |
| 3600-4002 | `podman run`, identity labels, host name and time zone, `ccy_handle_restart_exit`, compose stop prompt                                                                                | 400           |

### 2.2 The libraries (11,418 lines)

| File              | Lines | Content                                                                                                                                                                                                                                                                               | Who calls it |
| ----------------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| TM                | 2,047 | usage limits fetch/cache/render (160-1045, about 890), token list/validate/create (1046-1497), `select_token` (1498-1795), export (1816-2047)                                                                                                                                         | L, **CC**    |
| SSH               | 2,044 | GitHub alias and identity probing (50-380), key menu and selection (382-745), probe agent (744-900), restore askpass and passphrase supply (902-1056), fingerprints and the one-key agent filter (1057-1330), `build_ssh_mounts_and_validate` (1387-1807), commit signing (1808-2044) | L            |
| NM                | 1,318 | network preference files, `connect_to_network`, `disconnect_from_network`, compose offer and `up` (840-1250), DNS                                                                                                                                                                     | L            |
| SR                | 1,414 | session records and replay args (live, about 575), restore manifest, verdict, transcript, restore (dead, about 750)                                                                                                                                                                   | L, CC, TS    |
| common            | 1,099 | engine wrapper, SELinux/relabel preflight (46-385), git and `.gitignore` safety, `.env.local` dist, hostname allow-list, container version/hash, build                                                                                                                                | L            |
| dockerfile-custom | 776   | `--custom`, `--custom-docker`: the long prompt texts handed to Claude and the guided Dockerfile flow                                                                                                                                                                                  | L            |
| docker-health     | 651   | zombie/stale container TUI, `--top`                                                                                                                                                                                                                                                   | L            |
| TS                | 584   | `ccy_tmux_insulate`, offer, pick, `ccy_tmux_start_detached`, banner (live); take-over chain (dead)                                                                                                                                                                                    | L, CC, NM    |
| common-pure       | 574   | pure verdicts and validators: SELinux, rootless, GPU flags, mount lines, hostname, time zone, headless/unattended                                                                                                                                                                     | L, CC, TM    |
| restart-request   | 443   | supervisor restart contract: untrusted request file, budget, relaunch args                                                                                                                                                                                                            | L            |
| session-lifecycle | 262   | `--max-age`, `--run-for`, `--until`                                                                                                                                                                                                                                                   | L            |
| agent-bus-seats   | 206   | `--teams` seats                                                                                                                                                                                                                                                                       | L            |

Also part of the program and not bash: `helpers/ssh_agent_filter/ssh_agent_filter.py` (the one-key
SSH agent, started by `ccy_agent_filter_start` with `python3 -I`; it has its own pytest suite) and
`supervisor-plugins/ccy_lifecycle.py` (inside the container). Both can stay Python (decision D6).

### 2.3 CC, the host launcher (382 + 225 lines)

CC sources `common-pure`, `token-management`, `tmux-session`, `session-registry`. It does: tty check,
tmux insulation (`CCY_TMUX_SESSION_PREFIX=cc`), `--no-restore` removal, `--teams` for host seats
(`cc-teams.bash`), the named-token chooser (`select_token` in host mode), the onboarding flag seed in
`~/.claude.json` (jq), and **parking the host credential file** while a named token is used, with a
self-heal and a refusal if both the credential and its backup exist. It then runs `claude` (deliberately not
`exec`, so the EXIT trap can restore the credential). CC is small, but it is the largest reason the token
code must be ported **once, for both launchers**: TM is shared.

## 3. Host or container: where Go would have to run

Everything the launcher does happens on the host, before and after the container exists:

- Host: `claude-yolo` (installed root-owned at `/var/local/claude-yolo/claude-yolo`, mode 0755, by
  `play-claude-yolo.yml` "Copy Wrapper Script to /var/local/", with the libraries copied beside it),
  the shell `alias ccy=...` (`files/home/bashrc-includes/claude-yolo.bash.j2`), `cc` (`alias cc=...`),
  the registry records (`launcher=<absolute path>` is stored in every record, and ccy-sessions
  restore starts that path), the tmux trampoline, the token files under `~/.claude-tokens/ccy/`.
- Container: `entrypoint.sh`, baked into the image by `COPY` from the staged `/opt/claude-yolo/`
  build context, plus the supervisor and its plugins. **None of these call the launcher or the libraries.**
  The container's only inputs from the launcher are: the environment names passed with `-e`
  (`CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN` by name only, `GITHUB_USERNAME`, `DEBUG_*`, `SSH_KEY_PATHS`,
  `SSH_AGENT_FORWARDED`, `SSH_CONFIG_EXTRA_B64`, `SSH_KNOWN_HOSTS_PINS`, `GITHUB_SSH_443`,
  `CCY_HOST_HOSTNAME`, `TZ`, `CCY_CLAUDE_WRAPPER`, `CCY_NO_SUPERVISOR`, four `CCY_LIFECYCLE_*`,
  the Claude Code variables, `TERM`/`COLORTERM`/`FORCE_COLOR`), the mounts, the five `ccy*` labels
  and the command `claude --dangerously-skip-permissions ...`. That list is a wire contract
  with `entrypoint.sh` and must be byte-stable across the port.
- **Consequence**: no Go in the image, no `REQUIRED_CONTAINER_VERSION` or Dockerfile label bump for a launcher
  port, no change to what a project's custom Dockerfile builds `FROM`. (Moving a Go binary into the image
  would force a base-image rebuild for every ccy user; nothing in this port needs it.)
- The launcher is currently also the **integrity-hashed unit**: `CCY_HASH` is an md5 over the launcher text and
  every `lib/*.bash`/`*.py`; Quick Launch files store it; `scripts/git-hooks/pre-commit` demands a
  `CCY_VERSION` bump when any of those files change. A Go launcher has no source text to hash
  at runtime (decision D3).
- Deployment, once it is Go: same method as ccy-teams/ccy-sessions: build in the shared
  `~/.cache/ccy-go-build` scratch directory as the login user, `ansible.builtin.copy` into the root-owned
  directory with `become`, installed only when the checksum differs. `play-claude-yolo.yml` already
  imports `play-golang.yml` and builds both binaries.

## 4. What the existing Go packages already cover

| Package                                                    | Covers now                                                                                                                                        | Launcher use                                                                                     | Missing for the launcher                                                                                                                                       |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `term`                                                     | colour-on-tty, `PrintError`, `Say`, `Step`, `AbbrevHome`, exit codes                                                                              | every message                                                                                    | `read`-style prompt helper with bounded re-prompt (the launcher uses plain `read -rp`, not gum)                                                                |
| `ui`                                                       | gum confirm/input/choose/spin/box, fzf `Picker`, `Header`                                                                                         | the pickers (key, token, network menus are `read`-based today, see D5)                           | numbered-menu prompt with retry loop                                                                                                                           |
| `execx`                                                    | `Runner`, `OS`, `Fake`, `Cmd` (Stdin/Stdout/Stderr/Env/Dir), `Combined`, `ChildEnv` (add/unset), `Exec` seam                                      | `podman`/`docker` calls, git, ssh-add, curl replacement, final `exec`                            | foreground child with inherited tty and signal forwarding (`podman run -it`); env-name-only `-e VAR` support; process-group handling                           |
| `tmuxccy`                                                  | socket `ccy`, list/clients/detached/attach, `Panes`, `CapturePane`, `SendKeys`, `KillSession`, `StartDetached`, single-attach hook, refusal types | insulation, offer, start, banner, current session                                                | `ProjectSessions`, `NextName`, `Insulate`, the offer picker (TS 63-90, 251-430, 431-540)                                                                       |
| `registry`                                                 | record `Write/Get/List/SetRestore/Remove/ReadFile`, `Dir/Locate`; `RestoreArgs`, `ResumeID`, `Supervised`, trampoline (`restore.go`)              | record write and removal on session start                                                        | launch-args and replay-args (`ccy_registry_launch_args`, `_flag_class`, `_replay_args`, the four flag lists), `ForgetNetwork`, `RecordSSHKey`, `RecordCompose` |
| `sudo`                                                     | ready/ask/run as root                                                                                                                             | `ccy_seats_take` (agent-bus add-member)                                                          | nothing                                                                                                                                                        |
| `systemd`                                                  | homeserver units, `systemd-run` launch                                                                                                            | `ccy_tmux_start_detached` (partly in `tmuxccy`)                                                  | nothing                                                                                                                                                        |
| `gitx`                                                     | `ProjectHere`                                                                                                                                     | `is_git_repo`, `check_git_repo`                                                                  | remote URL, owner/repo parse, signing config, `.gitignore` safety, worktree preflight                                                                          |
| `agentbus`, `declare`, `marker`, `teams`                   | `--teams` flows of ccy-teams                                                                                                                      | `lib/agent-bus-seats.bash` (206 lines): `ccy_seats_check/resolve/take`, canonical `--teams` list | the seats library; much of the logic exists in `teams` and should be shared, not copied                                                                        |
| `restore`, `screen`, `transcript`, `sessprobe`, `sessions` | restore manifest, screen reading, transcripts, process joins, the ccy-sessions commands                                                           | **none** (the launcher never calls them)                                                         | nothing; this is the reason the SR restore chain is dead                                                                                                       |

Not covered at all and needing new packages: the container engine layer (`image`, `build`, `ps`, `inspect`,
`network`, `commit`, `stats`, the rootless check, the SELinux verdict and relabel preflight), token files and
selection, the usage-limits client (HTTP), SSH key discovery/probe/mounts/one-key agent/askpass, network
selection and connect/disconnect, compose, custom Dockerfile flows, the flag parser and Quick Launch,
the restart contract, session lifecycle, and the run assembly.

Proposed new packages (names are working titles): `internal/engine` (container engine runner, rootless,
SELinux), `internal/tokens` (files, expiry, validity, select, export, create, usage), `internal/sshkeys`,
`internal/netsel`, `internal/compose`, `internal/launch` (flags, Quick Launch, run assembly, restart),
`internal/lifecycle`, `internal/seats` (shared with `teams`), `cmd/ccy`, `cmd/cc`.

## 5. Which library functions are live, shared, or dead

Method: every function defined under `lib/` was matched by word against the launcher, the other libraries, CC,
`cc-teams.bash`, `ccy-teams`, `src-go/`, `helpers/`, `playbooks/`, and `scripts/`, with comment lines and the
defining line removed. The result for the three groups the task named:

### 5.1 Used only by the launcher (and CC), alive

Most of TM, SSH, NM, common, common-pure, dockerfile-custom, docker-health, restart-request,
session-lifecycle, agent-bus-seats, and in SR: `ccy_registry_dir`, `_wants_restore`, `_launch_args`
(L's tmux path and CC), `_flag_class` (also `restart-request.bash`), `_replay_args`, `_write`, `_forget_network`
(NM), `_record_ssh_key` (L 1357), `_record_compose` (NM), `_remove`, `_read`, `_trampoline`, and `ccy_restore_passphrase_take` (L 1220)
with `ccy_restore_passphrase_check`. In TS: `ccy_tmux_insulate`, `_offer`, `_row`, `_header`, `_pick`, `_teams_arg`,
`_session_seats`, `_seats_shared`, `_project_sessions`, `_next_name`, `_attach` and `_is_detached`
(the latter two through `insulate`), `_single_attach_hook`, `_start_detached`, `_current_session`, `_banner`, `_list`.

### 5.2 Shared with CC (must be ported once, for both)

`print_error`, `get_project_name`, `is_token_valid` (common-pure); all of TM's chooser and usage code;
`ccy_tmux_insulate`, `ccy_tmux_banner`, `ccy_registry_launch_args`, and what they call (TS, SR).

### 5.3 Dead after the teams and sessions ports (verified)

Nothing outside `scripts/` calls these; Go replaced them. Each is called by tests only.

| Function(s)                                                                                                                                                                    | File               | Lines (approx.) | Only caller                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------ | --------------- | ------------------------------------------------------------------------- |
| `ccy_tmux_take_over`                                                                                                                                                           | TS 204-250         | 47              | `test-ccy-sessions-take-over.bash` (a `declare -F` check at its L232-237) |
| `ccy_tmux_client_words` (calls `ccy_tmux_idle_words`)                                                                                                                          | TS 155-184         | 30              | only `ccy_tmux_take_over`                                                 |
| `ccy_tmux_idle_words`                                                                                                                                                          | TS 155-172         | 18              | only `ccy_tmux_client_words`                                              |
| `ccy_registry_set_restore`                                                                                                                                                     | SR 486-520         | 35              | tests                                                                     |
| `ccy_registry_restore_args`                                                                                                                                                    | SR 629-708         | 80              | only `ccy_registry_restore`; tests                                        |
| `ccy_registry_manifest_path`, `ccy_boot_id`, the `ccy_restore_manifest_*` family (locked, index, update, merge, write, read, rewrite)                                          | SR 709-979         | 270             | only `ccy_registry_restore`; tests                                        |
| `ccy_registry_resume_id`, `ccy_registry_supervised`, `ccy_restore_verdict`, `ccy_transcript_dir/_context_tokens/_took_input_since/_compacted_since`, `ccy_restore_going_state` | SR 980-1235        | 255             | `ccy_registry_restore`, tests only                                        |
| `ccy_registry_restore`                                                                                                                                                         | SR 1305-1414       | 110             | tests only                                                                |
| `ccy_known_prompts`                                                                                                                                                            | common-pure 70-113 | 44              | only `ccy_restore_verdict`; the Go twin is `screen.KnownPrompts`          |

The reportedly-dead trio is confirmed: `ccy_tmux_take_over`, `ccy_tmux_idle_words`,
`ccy_tmux_client_words` have no caller left but the suite that sources the library. Note that
`qa-all.bash` (L874-879) still runs that suite as a gate, and `test-ccy-session-registry.bash` (1,075 lines)
is mostly golden cases for this dead chain; the Go ports of their cases already exist
(`registry/restore_test.go`, `restore/manifest_test.go`, `transcript/transcript_test.go`).
`ccy_tmux_is_detached` and `ccy_tmux_attach` are NOT dead (the launch's insulation uses them). Total dead:
about 875 lines (5.7% of the program).

Deleting them is a library change, so it needs a `CCY_VERSION` bump (patch) and removes bash cases from
`test-ccy-session-registry`, `test-ccy-sessions-take-over`, `test-ccy-tmux-targets` and `test-ccy-sessions-reboot`
(the latter reads `ccy_transcript_dir`, `ccy_restore_manifest_*` from the bash library as a fixture; its
Go-side writes are already tested against the same format).

## 6. The risky areas

Each of these is a place where the bash behaviour is subtle, security relevant, or only observable on the host.

01. **The `podman run` command line (L 3860-3900) and every other engine call (71 `container_cmd` sites).**
    The argv is built from about 20 arrays and variables (`DOCKER_FLAGS`, `NETWORK_FLAG`, `GPU_DEVICE_FLAGS`,
    `DOCKER_MOUNTS`, `SSH_MOUNTS`, `SSH_RUN_OPTS`, `GUI_MOUNTS`, `CCY_SEAT_RUN_ARGS`, labels, 25 `-e` options).
    Word splitting is relied on in places (`$DOCKER_FLAGS`, `$NETWORK_FLAG` are deliberately unquoted).
    The container is run in the foreground, not `exec`ed, so the launcher can look at the exit status
    (75 means "supervisor asked for a restart") and offer compose shutdown afterwards. A Go version must keep
    the child in the terminal's foreground process group, forward nothing it should not, and return the
    child's status exactly. Docker and Podman differences (rootless verdict, `--userns`, `--network`,
    cross-engine mismatch) are decided inline.
02. **Tokens and secrets.** Token files live in `~/.claude-tokens/ccy/tokens/` at mode 600; the OAuth token
    is passed to the container **by name** (`-e CLAUDE_CODE_OAUTH_TOKEN`, `-e GH_TOKEN`, exported first) so that
    it never appears in `/proc/<pid>/cmdline`; the usage client feeds the bearer header to `curl --config -`
    on stdin for the same reason; token length and prefix checks (`sk-ant-oat01-`, 90 to 120 bytes);
    `token_shadow_warning`; atomic file writes with `chmod 600`; CC parks `.credentials.json` and
    restores it from an EXIT trap with a documented self-heal and a refusal state. Go must keep every one of
    these properties: never put a secret in argv or in a log, never in an error string, hold it only in
    memory and in the child's environment. A Go HTTP client for the usage report removes the curl argv issue
    but must keep the same cache files (`$XDG_CACHE_HOME`-style directory of `.result` files, three lines each),
    timeouts, and status semantics (`000` for no response, values kept from any status that carries headers).
03. **The one-key SSH agent filter and key handling (SSH, about 2,000 lines).** Deciding whether a
    passphrase-protected key can be forwarded from the user's agent instead of mounted (`ssh-add -l -E sha256`
    fingerprint match), starting `ssh_agent_filter.py` on a socket in `$XDG_RUNTIME_DIR` with `--parent-pid`,
    waiting for the socket in a 0.1 s loop, verifying the filtered agent lists every allowed fingerprint, and
    always stopping it on exit, including on SIGTERM and SIGHUP. Plus key probing against GitHub (`ssh -T`),
    per-key askpass staging with a passphrase file for the entrypoint (removed once the keys are added),
    and key copies staged into a per-session tmpfs directory on SELinux hosts. Wrong behaviour here either
    leaks a key to the container or hangs a restore at a prompt nobody can answer. The Python filter itself can
    be kept (D6).
04. **SELinux relabel and mount options (common 46-385, common-pure 170-295).** `ccy_selinux_mode` asks
    `getenforce`/`/sys/fs/selinux`, a verdict function decides `z`, `Z` or nothing; `workspace_relabel_preflight`
    refuses or repairs a workspace whose files are owned by a foreign uid (relabelling a tree one does not own
    fails halfway and leaves it inconsistent) and `relabel_foreign_owner_args` computes the repair. This is
    verified only by the host's real SELinux; the pure verdicts are unit-tested and port cleanly, the
    preflight is not.
05. **Rootless guard.** `engine_assert_rootless` asks the live engine and refuses to continue unless it is
    rootless; pure verdict in common-pure. Port the verdict with its table; the live call needs a fake engine.
06. **The restore trampoline and the registry records the Go tools read.** The launcher writes the record
    (`launcher=`, `prefix=`, `restore=`, `compose=`, `arg=` lines, mode 0600, temp file then rename) before starting the tmux session
    and the trampoline (`"$@"; rc=$?; rm -f <record>; ... read -r ...`) removes it when the launcher returns.
    `ccy-sessions` and `ccy-teams` (Go) read these files and ship `registry.Write` in the same format;
    `TestRegistryTrampolineAndStartAgree` already pins Go-written trampoline text to the bash form. Risks:
    (a) a record whose `launcher=` points at a path that stops existing makes the next boot's restore fail for that session
    (keep the path, D2); (b) the trampoline's wording is asserted by `verify-restore` and by the suites; (c) records
    are replayed with flag classification (`ccy_registry_flag_class`, the four flag lists) that must match
    the Go `RestoreArgs`; both sides must derive them from one Go table once the launcher is Go (SPEC-ccy-sessions 9.13 already
    marks the trampoline text for correction at that point); (d) a launch started by
    `ccy-sessions restore` is recognised only by `CCY_SESSION_RESTORE=1`, which L reads once and unsets.
07. **Signals, traps and the terminal.** The launcher relies on bash's EXIT trap, which also runs on SIGTERM and
    SIGHUP (a comment at L 1366 depends on this for a reboot's TERM): staged key copies, the temp git config,
    the restore passphrase copy, the probe agent, the askpass supply and the one-key agent are all removed
    there. It saves and restores `stty` (`susp undef` while the container runs), uses `stty -g`, and the
    restart path does `trap - EXIT; cleanup; exec "$SCRIPT_DIR/claude-yolo" ...` so that the relaunch does not
    inherit staged state. Go has no EXIT trap: it needs one cleanup stack run from `defer`, `signal.Notify`
    (TERM, HUP, INT, QUIT) and before every `syscall.Exec`, and `os.Exit` must never be called with cleanups
    pending. `read` prompts need the tty and must give up cleanly on EOF.
08. **Interactive volume.** 51 `read` prompts, numbered menus, `[Y/n]` questions with specific defaults, and
    "nobody to ask" behaviour for headless, restore and restart launches (`ccy_nobody_to_ask`,
    `ccy_prompt_refuse`: one safe answer or a refusal). CLAUDE/InteractiveScripts.md applies (validate strictly,
    re-prompt in a bounded loop). The existing suites drive these with piped stdin; a Go port keeps the same
    plain line prompts first (D5).
09. **Network selection (about 780 inline lines plus 1,318 in NM).** Saved preference files per project,
    auto-connect, cross-engine mismatch, DNS aliasing, `--network`, `--no-network`, `--connect`, `--disconnect`, compose
    detection and `up`/`down` with outcomes recorded in the session record. Largest block of untested inline code.
10. **Custom Dockerfile flows and the daily image update.** `--custom`/`--custom-docker` hand large prompt texts to
    Claude (`dockerfile-custom.bash`, mostly text that can be `//go:embed`ded); `update_claude_inplace` does
    `run`, `commit --change ENTRYPOINT/CMD`, `rm` and must clean the temp container on every path.
11. **Text asserted by suites and tooling**: help text; `--version` output `ccy version X` and `Hash:`; error
    sentences; all exit codes (1 generic, 64 usage, 75 restart, container status passthrough).

## 7. The test suites as the parity oracle

Inventory (34 `scripts/test-ccy-*.bash`, 12,651 lines):

| How the suite reaches the code under test                                 | Suites                                                                                                                                                                                                                                       | Survives a Go launcher?                                                                                |
| ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Runs the launcher as a program (`bash "$LAUNCHER" args`)                  | `info-flags`, `token-usage-command`, `teams` (argument validation only)                                                                                                                                                                      | yes, with the binary path substituted                                                                  |
| Extracts function or block text from the launcher with `awk`/`grep`/`sed` | `flag-check`, `lifecycle`, `restart-request`, `restore-askpass`, `compose-restore`, `container-menus`, `git-signing`, `gpu-device`, `host-time-zone`, `relabel-preflight`, `session-registry`, `teams`                                       | **no**: needs a Go unit-test equivalent                                                                |
| Sources a library and calls its functions (about 24 suites)               | `token-mode`, `token-usage-output`, `token-import-cleanup`, `ssh-handling`, `ssh-probe`, `network-disconnect`, `selinux-verdict`, `rootless-guard`, `host-hostname`, `env-local-dist`, `gitignore-safety`, `project-env`, and the ones above | **no**: the function is gone; port the cases as Go tests, keep the bash cases while the library exists |
| Needs no launcher code (entrypoint, other)                                | `git-preflight`, `container-version-hook`, `agent-bus` (entrypoint), parts of `teams`                                                                                                                                                        | unaffected                                                                                             |

Consequences:

- The launch path (everything from L 1230 on) has **no black-box test**. The 12 suites that cover it do so by
  extracting text, which proves the shape of the bash, not the behaviour of a launch.
- The libraries' unit suites are good golden-case sources (SPEC-ccy-sessions 10.2 made the same point): each
  case becomes a Go table test in the new package, and the bash case stays until the library function is
  deleted. This is real work (12,651 lines of suite) but mechanical.
- **Required before porting the main path: a differential harness.** A new suite (working name
  `scripts/test-ccy-launch-parity.bash`) puts fakes of `podman`/`docker`, `ssh-add`, `ssh`, `git`, `tmux`, `curl`, `gum`,
  `systemd-run`, `getenforce`, `stty` first on `PATH`; each fake appends its argv and the **names** (never the values of
  secrets) of the environment variables it was given to a log, and answers from a script. The suite runs the bash
  launcher over a matrix of scenarios (about 40: plain launch, `--token`, `--ssh-key`, `--no-ssh`, `--network`,
  `--no-network`, headless, restore, restart relaunch, SELinux enforcing, docker vs podman, compose, custom
  Dockerfile, rebuild, update) in a scratch `HOME`, records the log and the files written (Quick Launch file, session
  record, git config copy), and stores them as golden files. The same matrix then runs against the Go binary and
  compares. It can be built and made green on the bash launcher **before any Go exists**, which is also what makes
  it a safe baseline, and it runs in the container (no real engine needed). It cannot prove a real engine, a real
  SELinux or a real terminal; those stay a host acceptance run (section 8, each phase).
- Black-box suites already there must keep their names so `qa-all.bash` and `qa-changed.bash` keep their
  wiring; their `LAUNCHER` becomes a build step (as the teams suite does).

## 8. Phased plan

Rule for every phase: bash launcher keeps working (strangler, D1); the phase ships when its acceptance holds; the plan
file, `docs/ccy.md`/`docs/ccy-changelog.md`, and `CCY_VERSION` move with the code (any change under
`files/var/local/claude-yolo/` bumps the version; a Go-only change to `src-go/` does not, but the cut-over of an entry point does);
`qa-all.bash` green; qa-reviewer run; the owner's host run via `meta-deploy.bash` for any phase that changes what the
play installs. Each phase's Go code is unit-tested before its cut-over (fake `Runner`, golden cases ported from the
suites).

### Phase 0: preparation (no Go launcher yet)

- 0.1 Delete the dead bash (section 5.3): the take-over chain, the SR restore chain, `ccy_known_prompts`; remove or shrink
  the suites that only tested them; bump `CCY_VERSION`. Acceptance: `qa-all.bash` green; the Go suites for the same
  behaviour (`sessions`, `restore`, `transcript`, `registry`) are untouched and pass; no grep hit for any deleted name
  outside git history; `ccy` still starts a session on the host.
- 0.2 The differential harness (section 7) on the bash launcher: scenario matrix, fakes, golden logs, a runner that
  takes `LAUNCHER=<path>`. Acceptance: green against the bash launcher; a deliberate one-flag change in the launcher
  makes it red (prove the oracle can fail).
- 0.3 Decisions D1 to D6 recorded in the plan.

### Phase 1: foundation (new packages, nothing replaced)

- 1.1 `internal/engine`: engine selection (`CCY_CONTAINER_ENGINE`/`--engine`), a runner for `image inspect`, `ps`, `rm -f`,
  `network ls`; the rootless verdict and the SELinux verdict as pure functions with the cases of `rootless-guard` and
  `selinux-verdict`; the relabel preflight pure parts (`relabel-preflight`). Acceptance: the golden cases of those three
  suites pass as Go tests.
- 1.2 `execx` additions: foreground child with inherited tty and signal handling, env-name-only passing, a cleanup
  stack; `term`/`ui` bounded-retry line prompts. Acceptance: unit tests, including a process-group test with real
  `sh` children and a SIGTERM during a child.
- 1.3 `internal/tokens`: token file names and expiry, `is_token_valid`, `colorize_expiry`, `token_shadow_warning`,
  `list_tokens`. Acceptance: ported cases of `token-mode`, `token-import-cleanup`.

### Phase 2: standalone modes (smallest safe slices; each is a cut-over of one flag)

Install the Go binary as `claude-yolo` at the existing path with the bash launcher renamed beside it (D1, D2); the Go
`main` handles only these flags and otherwise `exec`s the bash launcher with the original argv and environment.

- 2.1 `--version/-v` and `--help/-h` (embedded help text). Acceptance: `test-ccy-info-flags.bash` unchanged and
  green against the binary; `--help` output byte-identical to bash; `--version` prints a hash by the rule chosen in D3.
- 2.2 `--token-usage [--json]` and the usage cache (HTTP in Go). Acceptance: `test-ccy-token-usage-command.bash`
  and `test-ccy-token-usage-output.bash` cases pass; the token never appears in a process argv or in the cache; JSON
  and table output byte-identical to bash for a recorded set of response headers (golden files); same exit codes
  (64 on any option other than `--json`).
- 2.3 `--list-tokens`, `--export-token` (both forms). Acceptance: ported cases; token files untouched at mode 600.
- 2.4 `--prevent`, `--top` (`docker-health`, 651 lines of TUI-ish listing), `--connect`/`--disconnect` (NM part).
  Acceptance: `container-menus`, `network-disconnect` cases ported; the `--top` stop actions run only after a typed
  confirmation exactly as before.
- 2.5 `--create-token`, `--update-token` (interactive, runs `claude setup-token` in a container, writes a token file).
  Acceptance: ported cases; a failure never leaves a half-written token file.

### Phase 3: cc

- 3.1 `cmd/cc` on `internal/tokens` (the chooser in host mode), the credential parking with its cleanup stack, the
  `~/.claude.json` onboarding seed (without jq, with the same "refuse if unparsable" behaviour), `--teams` for host
  seats (shared `internal/seats`), tmux insulation call. Needs `tmuxccy.Insulate` and `registry` launch/replay args
  (the same pieces the launcher needs in phase 4, so do them here, once). Acceptance: a new CC test suite (CC has none
  of its own today beyond `teams` flows) covering the park/restore/self-heal/refusal matrix with a fake `claude`, run
  against bash first, then Go; the owner's host run proves the chooser and a real token session.

### Phase 4: the main launch path (the 3,000 lines that remain), in dependency order

Each step moves one block into Go behind the same strangler: Go runs the new block, then hands the rest to the bash launcher
through a documented, narrow interface (D7) until the last step removes the hand-over. The differential harness is the
gate for every step.

- 4.1 Flag parser, Quick Launch load/save, launch configuration struct, the "suspect flag" check, `--debug` chooser.
- 4.2 Token selection (`select_token`, with the usage block) and the validation-failure recovery UI.
- 4.3 SSH keys: discovery and selection, agent forwarding decision, probe, mounts, askpass staging, the one-key agent
  start/stop (keeps `ssh_agent_filter.py`), commit signing.
- 4.4 Image: existence, version/hash check, rebuild, project Dockerfile build, daily update and `update_claude_inplace`,
  `repair_project_state_permissions`, custom Dockerfile flows (`--custom`, `--custom-docker`).
- 4.5 Network selection and compose (the largest and least covered block; port with its own scenarios added to the harness first).
- 4.6 Run assembly and the container run, identity labels, host name and time zone, restart contract
  (`ccy_handle_restart_exit`, the budget, relaunch), lifecycle flags, seats, compose stop prompt, cleanup stack.
- 4.7 Tmux insulation and registry writes from Go (`tmuxccy.Insulate`/`NextName`/`ProjectSessions`, `registry` launch and replay args),
  then the Go binary is the only launcher: delete the bash launcher, `lib/*.bash`, the sourcing suites whose cases now live in Go,
  `CCY_LIBS`, the library copy tasks in the play, and the `lib` part of the integrity hash and the pre-commit hook.
  Acceptance: the harness matrix is green on the Go binary alone; every ported case is in a Go test; the owner's host run
  starts, detaches, re-attaches, reboots (restore) and restarts a real session on both Podman and, if still used, Docker.

### Phase 5: close

- 5.1 `docs/ccy.md`, `CLAUDE/ContainerRules.md` (the version-bump section changes meaning), `CLAUDE/QA.md`, the pre-commit hook,
  `scripts/qa-all.bash`/`qa-changed.bash` wiring; qa-reviewer over the whole port.

## 9. Open decisions for the owner

- **D1. Replacement strategy.** (a) Strangler at the same path: Go at `/var/local/claude-yolo/claude-yolo`, bash
  renamed and `exec`ed for unported flags (recommended; no alias change, no registry migration, one-play rollback);
  (b) a new binary name with an alias switch at the end (simpler binary, but two entry points for the whole period and
  records pointing at the old path); (c) a big-bang port behind the differential harness (one cut-over, no
  hand-over interface, but nothing ships for the whole duration).
- **D2. Install path and records.** Every existing registry record names the launcher by absolute path. Keep
  `/var/local/claude-yolo/claude-yolo` as the path of the final program (recommended), or accept that records
  written by the bash launcher need a one-time rewrite. Also: root-owned `/var/local/` copy by `become` (current), or a
  per-user install in `~/.local/bin` like the other two tools (would change the alias, the VM acceptance script and the
  trampoline).
- **D3. Version and integrity hash.** `CCY_VERSION` and `CCY_HASH` currently derive from the source text and gate Quick Launch
  files and the pre-commit hook. A binary can carry the version in `-ldflags -X` and hash itself (`os.Executable` +
  sha256) or the build can embed a hash of `src-go/` inputs. Decide whether Quick Launch files keep a hash check,
  and whether the pre-commit "bump `CCY_VERSION` when launcher or lib changes" rule moves to `src-go/` paths.
- **D4. Scope of "ccy".** Whole launcher including network, compose and custom-Dockerfile flows (about 4,000 lines of
  bash, of which `dockerfile-custom` is mostly prompt text), or stop after phase 3 plus the token, SSH and run core and leave
  `--custom`, `--custom-docker`, `--top` and the zombie/stale-container TUI in bash behind the hand-over? The strangler allows
  stopping at any phase boundary; the hand-over then becomes permanent.
- **D5. Prompt style.** Keep plain line prompts and numbered menus (parity, the suites pipe stdin), or move menus to
  gum/fzf like ccy-teams (better UX, but the suites that pipe answers then need new fakes). Recommended: plain first, UI later.
- **D6. Python pieces.** Keep `ssh_agent_filter.py` (Python, own tests, started with `python3 -I`) and the container
  supervisor as they are (recommended, consistent with the plan's non-goals), or port the agent filter.
- **D7. Hand-over interface during phase 4.** Go computing part of the launch and bash the rest needs a defined
  interface (environment variables, or a file of `NAME=value` lines the bash side sources). Alternative: skip
  partial hand-over inside phase 4 and port 4.1 to 4.6 as one cut-over (the harness makes this feasible) while the standalone
  modes and cc still ship incrementally. Recommended: no partial hand-over inside the launch path.
- **D8. Library deletion timing.** Delete the dead bash (phase 0.1) now, independent of the rest, or leave it until the
  libraries go. Recommended: now; it removes oracles that test nothing and costs one patch version.
- **D9. Docker.** The launcher supports Docker as a compatibility engine (`--engine`, cross-engine network mismatch).
  Port both engines, or drop Docker support from the Go launcher (CLAUDE/ContainerEngines.md says Podman first, Docker only for
  compatibility such as DDEV)? Dropping it removes the cross-engine branch of the network block.
- **D10. Stopping point and value.** The earlier ports removed a specific failure class (functions bound at run time, tests
  only through a pty). The launcher has less of that pain visible today, and the highest bash defects found so far
  are in the interplay of globals. If the owner is not hitting launcher bugs, phases 0 to 2 (dead code, harness,
  standalone modes) deliver most of the testability gain for a small fraction of the work, and phases 3 to 5 can be deferred.

## 10. Risks of the plan itself

- The differential harness is only as good as its scenarios; budget its construction as a phase (0.2), not a footnote.
- A strangler with a partial hand-over can hide a state mismatch (a variable bash expected Go to have set). D7's
  recommendation exists for this reason.
- `ccy` is the tool the agents themselves are launched with; a defect in the launcher stops all of them at once. Keep the bash
  launcher present and `exec`able for the whole port, and keep one flag (`CCY_LEGACY=1` or similar) that routes everything
  to it, until phase 4.7 is accepted on the host.
- Test-count effect: porting the 24 sourcing suites moves several thousand lines of bash assertions into Go tables;
  the bash cases are deleted with their functions, never before the Go twin passes.
- Plan 00191 states a Go toolchain is a host prerequisite (installed by `play-golang.yml`); a launcher in Go makes the host
  unable to run `ccy` after a Go-less rebuild only if the binary was not built. The play builds before it installs, and
  fails fast, as for the two existing tools.
