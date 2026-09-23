# Plan 00137 research: the existing pieces an unattended server self-update would reuse

Read-only survey of the F44 tree. Every claim cites `file:line`. Anything inferred rather
than read is marked **UNVERIFIED**. No install-specific names appear here; the checkout path
`~/Projects/fedora-desktop` is the one `run.bash` itself hardcodes.

---

## 1. Repo update: how run.bash updates the checkout today

**There is no reusable "update checkout safely" helper.** The update is plain inline code in
`run.bash`, in three places:

| Path                                              | Code                                                                                                    | Lines                |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | -------------------- |
| Headless, `RUN_BASH_GITHUB_ACCOUNTS=none` (HTTPS) | `assert_clean_worktree`, force origin to the HTTPS URL, then `git pull` (skipped if `RUN_BASH_GIT_REF`) | `run.bash:2167-2191` |
| GitHub-configured (interactive and headless, SSH) | `assert_clean_worktree`, force origin to the SSH URL, then `command git -C … pull`                      | `run.bash:2415-2431` |
| Before the main playbook (both)                   | `assert_clean_worktree`, then `command git pull` (skipped if `RUN_BASH_GIT_REF`)                        | `run.bash:2789-2801` |
| `RUN_BASH_GIT_REF` (Plan 00106)                   | `hl_checkout_ref`: `git fetch origin`, then `checkout --detach <sha>` or `checkout -B <br> origin/<br>` | `run.bash:696-726`   |

How each case behaves:

- **Fast-forward only? No.** It is a bare `git pull`. `play-git-configure-and-tools.yml:26`
  sets `git config --global pull.rebase false`, and nothing sets `pull.ff only`. So a
  **diverged branch gets a local merge commit** and the run carries on. Nothing refuses it.
- **Dirty tree:** `assert_clean_worktree` (`run.bash:1233-1247`) runs
  `git status --porcelain`. Untracked files count as dirty. On any output it prints the fix
  and `exit 1`s. Gitignored host files (`environment/localhost/host_vars/localhost.yml`,
  `.ansible/`, `untracked/`) are ignored (checked with `git check-ignore`), so they never
  make the tree dirty. The repo-root vault password file is also expected to be ignored:
  **UNVERIFIED**, because the name is a protected path the guard would not let me query.
- **Detached HEAD:** a plain `git pull` fails there, and the comment at `run.bash:2177-2178`
  and `2790-2794` says so. That is why the `RUN_BASH_GIT_REF` path skips the pull.
  `hl_checkout_ref` with a branch name runs `checkout -B <ref> origin/<ref>`, which
  **resets the local branch to origin's tip**. That throws away local-only commits on it
  without asking (`run.bash:721`).
- **Branch/remote model** (`docs/development.md:55-99,163-186`): one branch per Fedora
  release, `F<VERSION>`, and the current one is GitHub's default branch (F44 today).
  `vars/fedora-version.yml` is the single source of the target version. After the default
  moves, local `origin/HEAD` goes stale until `git remote set-head origin --auto`
  (`:165-186`). The `F*` branches are PR-protected, but the owner has push-bypass
  (`docs/development.md:156-158`, `CLAUDE/AgentNotes.md:686-687`).
- **Remote protocol decides whether an unattended fetch can authenticate.** On the `none`
  path, origin is forced to HTTPS (`run.bash:2159,2172-2176`), which is anonymous and works
  from a timer. On the GitHub-configured path, origin is forced to SSH
  (`run.bash:2395,2421-2425`) with a passphrase-protected key. The key is unlocked only
  through a transient ssh-agent, which is killed straight after the last git operation
  (`run.bash:2803-2810`, `hl_ssh_agent_stop` at `:479-504`). `host-health-collect.service.j2:54-64`
  already records the consequence: a timer has no agent, so an SSH-remote checkout cannot
  fetch there.
- **Version gate after the update:** both paths check that `vars/fedora-version.yml`
  exists and matches the running Fedora (`run.bash:2194-2200` and `2434-2439` onward).
  An unattended updater needs the same gate. When the branch model moves to F45, a
  server still on F44 must not follow a pull into F45 content.
- `main()` wraps the whole body so a `git pull` that rewrites `run.bash` mid-run cannot
  corrupt the running script (`run.bash:821-829`, `3309-3312`). A new updater script needs
  the same protection, or it must run from outside the checkout.

## 2. Stale-play detection: helpers/play_ledger

### Getting a machine-readable list

- `check_freshness.run(..., judged=list)` (`helpers/play_ledger/check_freshness.py:53-178`)
  fills `judged` with **every** `Verdict(play, state, changes, successor)`, fresh ones
  included. It does this only on a complete judgement. On every untrustworthy path it stays
  empty (`:72-75,167-168`). This is a **Python in-process API**. The CLI `main()`
  (`:300-316`) prints only human text and has no JSON option.
- The only existing consumer is `login_report.main` (`helpers/host_health/login_report.py:542-567`).
  It writes the result into the status document as `plays: [{"play","state"}]` through
  `play_runner.runnable` (`helpers/host_health/play_runner.py:52-63`), which leaves out GONE
  plays. The document is `$XDG_STATE_HOME/fedora-desktop/host-status.json`
  (`helpers/host_health/status_document.py:79,257`). A consumer can read that file, but it
  is only as fresh as the last collection.
- Exit codes: `0` clean, `1` findings, `2` untrustworthy (`check_freshness.py:39-44`).

### What "stale" means, and its limits (load-bearing for this plan)

- **Only the play file is watched.** `git_history.changes_since` runs
  `git log <ledgered-commit>..HEAD -- <play>` (`helpers/play_ledger/git_history.py:56-83`).
  A change to `files/var/local/claude-yolo/*`, `lib/*.bash`, templates, `tasks/` or `vars/`
  does **not** make `play-claude-yolo.yml` stale unless the play file itself changed. The
  design says so outright: `DESIGN-play-ledger.md:39-46` ("no check may claim 'this play is
  unchanged', only 'this play file is unchanged'"). **For the main candidate play, the
  ledger will usually report FRESH after a CCY-only change.** A path-based
  `git diff --name-only OLD..NEW` → play mapping is needed instead of the ledger, or on top
  of it.
- **The comparison is against local `HEAD`, not the remote.** So the check must run
  **after** the checkout is updated. Before the pull it can only report staleness against
  the old HEAD.
- **A play whose last run failed reads as FRESH** when its file has not changed since.
  `freshness.classify` (`helpers/play_ledger/freshness.py:63-101`) never reads `outcome`.
  Only `_apply_retirements` does (`check_freshness.py:210-214`). A failed unattended run
  would therefore not be retried on the next tick unless the updater tracks failures itself.
- **Never-run plays are silent by design** (`freshness.py:9-11`; `ledger.genesis_record`
  `ledger.py:164-174`). A play newly added to `playbook-main.yml` never shows as stale.
  The ledger-presence check (`helpers/play_ledger/ledger_presence.py`, used at
  `login_report.py:544`) covers only a completely empty ledger.
- **GONE** is a play that is in the ledger but not at HEAD (`freshness.py:81-84`).
  `retired-plays.json` maps it to a successor (today only
  `play-claude-code.yml → play-claude-yolo.yml`), and the finding clears once the successor
  has run successfully at a commit without the old play (`check_freshness.py:181-219`).
- **Dirty-tree runs:** the ledger records `dirty` and `play_sha256` per play
  (`ledger.py:126-161`; `repo.is_dirty` counts untracked files, `helpers/play_ledger/repo.py:55-62`).
  A dirty run whose play bytes differ from HEAD is STALE. A clean run with differing bytes
  and no explaining commit is UNEXPLAINED (`freshness.py:89-101`).
- **BROKEN ledger:** a `BROKEN` sentinel (`ledger.py:90-98`) makes the check answer nothing
  and exit 2 (`check_freshness.py:91-96,231-244`). A corrupt line also exits 2
  (`:98-104`). The sentinel never clears itself. Operator route:
  `python3 -m helpers.play_ledger.check_freshness --clear-broken` (`:256-292`).

### Does the ledger record on a server?

Yes. The callback is enabled in `ansible.cfg:18-24` with no profile condition, so it
records any `ansible-playbook` run from the checkout (`callback_plugins/play_ledger.py:1-8`).
Two ways it records nothing: `ANSIBLE_CONFIG` pointing elsewhere (`:6`), and `--check`/
`--syntax-check`/`--list-*` runs (`:20-22`). Each imported play is recorded under its **own
file path** (`plugin_support.play_source` `helpers/play_ledger/plugin_support.py:72-87`).
So a `playbook-main.yml` run records `playbooks/imports/play-claude-yolo.yml` on its own.
Location: `$XDG_STATE_HOME/fedora-desktop/play-ledger/runs.jsonl`, falling back to
`~/.local/state/...` (`ledger.py:57-83`). The fallback matters, because a systemd user unit
normally has no `XDG_STATE_HOME`. A `gnome`-scoped play run on a server records a no-op run
(the scope guard `end_play`s after play start): **UNVERIFIED**, inferred from the collector
recording at `on_play_start`.

### Does check_freshness's fetch interact with a pull?

- `git_history.fetch` is `git -C <root> fetch --quiet` with `GIT_TERMINAL_PROMPT=0` and a
  20 s timeout (`git_history.py:36-53`). It **never moves the working tree** (`:6-9`, and
  test-asserted). By itself it cannot conflict with a pull's merge.
- A failed fetch is not fatal; the check judges on the refs it already has
  (`check_freshness.py:111-123`).
- **Concurrency, UNVERIFIED:** `host-health-collect.timer` fires daily at 06:15, with
  `RandomizedDelaySec=30min` and 5 min after boot (`host-health-collect.timer.j2:15-28`).
  It runs the same fetch. A concurrent fetch and pull on one repo can fail with a
  ref-lock error. The updater's timer should keep out of that window, or retry on that
  specific error.

## 3. Running plays unattended

### Why single-play mode is interactive-only

`run.bash:1122-1128` aborts with `fatal "single play" "a single play is interactive-only"`
when `PLAY_PATH` is set and `HEADLESS=true`. The rationale is in Plan 00114
(`CLAUDE/Plan/00114-run-bash-single-play-mode/PLAN.md:44-48`): headless already had
`RUN_BASH_OPTIONAL_PLAYBOOKS`, and `become_ask_pass` in `ansible.cfg` would hang a headless
run. The structural reason: turning headless on runs `headless_preflight`
(`run.bash:262-413`). That is the **whole provisioning contract**. It requires
`RUN_BASH_USER_EMAIL`, `RUN_BASH_GITHUB_ACCOUNTS`, the vault password file (on `none`,
`:340-343`), and the token and SSH passphrase files (on a GitHub path, `:349-363`). A
single play needs none of these.

What happens today if a timer runs a play through its shebang:

- Every play's shebang execs `run.bash <play>` (`playbooks/imports/play-claude-yolo.yml:1`).
  (`playbooks/CLAUDE.md` still documents the older `cd && exec ansible-playbook` shebang,
  which is doc drift.)
- **No `RUN_BASH_*` in the environment means `HEADLESS=false`** (auto-detect needs no-TTY
  **and** a `RUN_BASH_*` var, `run.bash:1086-1115`). So the single-play path runs "as
  interactive". It dispatches at `run.bash:1735-1760`, before any install step, and goes
  through `run_playbook_with_issue_option` (`:1677-1727`):
  - HL_SUDO_OPTS is empty (no preflight), so the run goes straight to the `sudo -k -n true`
    probe. On **NOPASSWD** it runs the play bare (`:1707-1708`). **That path already works
    unattended.**
  - On password sudo it goes to `--ask-become-pass` (`:1709-1711`). With no TTY that fails
    or hangs: **UNVERIFIED which**.
  - On play failure, `confirm "create a GitHub issue…" n` is reached. With stdin at
    `/dev/null`, `read` hits EOF, `yn` is empty, and the default `n` returns 1
    (`:1276-1285`), so the run exits with the play's status. **UNVERIFIED on a real
    systemd unit**; this is inferred from the code.
  - `command -v ansible-playbook` must succeed (`:1753-1756`). Ansible is a pipx shim in
    `~/.local/bin` (`run.bash:2045-2070`). A systemd user unit's default PATH is not known
    to include it (**UNVERIFIED**), so the unit must set `Environment=PATH=%h/.local/bin:…`.

What a headless single-play path would need: skip identity, GitHub and vault provisioning
preflight. Keep the sudo half: the NOPASSWD probe, or `hl_resolve_secret SUDO_PASSWORD` →
`hl_sudo_askpass_start` → `hl_sudo_probe_password` → `--become-password-file`
(`run.bash:369,394-403,232-256,1705-1706`). Never reach `confirm`/prompts. Alternatively,
bypass `run.bash` altogether and call `ansible-playbook <play>` from the repo root, as
`hl_run_optional_playbooks` does (`run.bash:808-816`).

### How headless supplies become

The preflight decides this once (`run.bash:376-412`):

- If `sudo -k -n true` passes, the run is NOPASSWD. `HL_SUDO_OPTS=()` and Ansible runs bare.
- Otherwise `RUN_BASH_SUDO_PASSWORD_FILE` is read into a `mktemp` 0600 copy with a 0700
  askpass helper (`:232-239`), the password is proven (`:250-256`), and
  `HL_SUDO_OPTS=(-A)` is set. Ansible gets `--become-password-file "$HL_SUDO_PW_FILE"`
  (`:2877-2879`, `:809-811`, `:1705-1706`).
- A command-scoped sudoers rule is unsupported on both routes
  (`docs/headless-server-install.md:51`).
- The temp copies are unlinked by `hl_cleanup` on exit (`run.bash:432-440`). The
  operator's own `*_FILE` files are **not** deleted. The docs put them on tmpfs
  `/run/secrets` (`docs/headless-server-install.md:85-87,128-134`), so **they are gone
  after a reboot**.

### First-install-only or wrong on a maintenance run (full headless `run.bash`)

Re-running full headless `run.bash` as the periodic job would, every time:

- require **all provisioning secrets present on disk**: the token, the SSH passphrase and
  the vault password (`run.bash:333-363`). On the documented tmpfs layout that is
  impossible after the first reboot. Persisting them on disk for a timer is a security
  decision.
- `dnf -y install` the base deps (`:2015-2030`); run `grubby` (`check_legacy_grub_cgroup`
  `:168-214`); run pipx install and inject (`:2036-2071`);
- set the hostname if `RUN_BASH_HOSTNAME` is set (`:2125-2145`); install gh, add its repo,
  and run `gh auth login --with-token` (`:2211-2290` approx.); check the SSH key upload
  (idempotent by key blob, `:2352-2366`); **rewrite `~/.ssh/known_hosts` GitHub entries**
  from `api.github.com/meta` (`:2375-2386`);
- reconcile `localhost.yml` (`hl_write_localhost_yml` `:593-649`) and the vault
  (`:651-694`, overwriting the vault password file from the supplied one, `:662-664`);
  run `gh-account-setup.bash --setup-all` (`:2765-2787`);
- run `ansible-galaxy install -r requirements.yml` (a supply-chain fetch, `:2812-2829`),
  then the **whole `playbook-main.yml`**, including `play-AB-dnf-upgrade.yml`;
- optionally restore projects and run optional plays, and reboot when `RUN_BASH_REBOOT=1`
  (`:2913-2935,3133-3134,3289-3298`).

`--optional-only` still runs the preflight (`:1130-1133` sits outside the
`OPTIONAL_ONLY` block, which starts at `:2008`). It skips the git pulls (they are inside
that block, ending at `:2941`). And `RUN_BASH_OPTIONAL_PLAYBOOKS` only resolves plays under
`playbooks/imports/optional/` (`:746`). **`play-claude-yolo.yml` is a core play in
`playbooks/imports/` and cannot be named there.**

### What play-claude-yolo.yml needs alongside it

- **A container engine and podman-compose**, asserted and not installed
  (`play-claude-yolo.yml:37-89`). Owned by `play-podman.yml`.
- **A running user manager**, for the restore-unit tasks
  (`play-claude-yolo.yml:742-826`). Linger and `user@UID` come from
  `play-systemd-user-tweaks.yml:23-50`.
- The play installs tmux and fzf itself (`:674-680`). `/etc/tmux.conf` comes from
  `play-tmux-sessions.yml` (`dest: /etc/tmux.conf`, line 23). A running ccy tmux server
  does not re-read that file (**UNVERIFIED**).
- `tasks/ensure-jq.yml` (`:447-448`) and `vars/container-defaults.yml` (`:33-35`).
  host_vars supply `user_login` and `ccy_restore_sessions` (`:772,780`).
- It deploys `/var/local/claude-yolo/claude-yolo` and `lib/` (`:324-362,413-424`), host
  `claude install latest` (`:485-510`), the `cc` wrapper, and `ccy-sessions` (`:696-705`).
  It always runs `podman build -t claude-yolo:latest /opt/claude-yolo` (`:853-870`), which
  needs network and time.
- **Vault:** the play reads no vaulted variable (it has no vault reference). But
  `ansible.cfg:46-53` sets `vault_password_file`, so any `ansible-playbook` start needs that
  file readable. On a server it persists at `~/Projects/fedora-desktop/<vault_password_file>`
  (0600, written by `run.bash:2658,662-664`), so a timer run finds it.

## 4. Sessions: warn, stop, restore, verify

### (a) Warn

- `ccy-sessions notify going-down --minutes N` (`files/home/.local/bin/ccy-sessions:199-233`).
  It picks the kind from the restore opt-in (`going_down_kind` `:189-197`, the wants-symlink
  test). `reboot-warning` means "a session restore will follow". `shutdown-warning` means
  "NO restore, leave a handoff". `notify reboot-cancelled` withdraws the warning.
- Delivery goes through each live project's own
  `.claude/hooks-daemon/bin/hooks-daemon signal <kind> --minutes N --all-sessions --project-root DIR`
  (`ccy-sessions:126-184`). **If any live project lacks the CLI, nothing is sent and the
  command exits 1** (`:153-166`). An unattended job must treat that as a refusal and not
  skip past it.
- The texts are fixed in the supervisor: `.claude/ccy/claude-supervise.py:3118-3142` (the
  reboot text at `:3122-3127`, shutdown at `:3128-3134`, cancel at `:3135-3139`). Only
  `minutes` is interpolated. The chat injection happens **at the idle choke point**
  (`:79-94`), with an immediate status-line countdown. A session busy mid-turn sees the
  chat line only when it next idles.
- **Neither text fits "update, restart the sessions, no reboot."** `reboot-warning` says
  "the host machine will reboot", and there is no kind for "the sessions will be restarted
  in place". Plan 00135 T3.7 already records this verb mismatch as a hooks-daemon change
  that has not been filed (`CLAUDE/Plan/00135-ccy-sessions-survive-a-reboot/PLAN.md:182-186`).
- **There is no acknowledgement channel.** Nothing tells the host that an agent has
  committed, journalled and gone idle. `ccy-sessions reboot` just waits out N minutes
  (`ccy-sessions:262-317`).
- A session started with `--no-supervise` has no supervisor, so the chat line has nothing
  to inject it (`claude-yolo:240-241,3062-3072`). **UNVERIFIED** whether the status-line
  countdown still renders in that case.

### (b) Stop while keeping the records

- The record lifecycle is in `files/var/local/claude-yolo/lib/session-registry.bash:1-11,297-311`.
  The pane runs the trampoline
  `"$@"; rc=$?; rm -f -- <record>; …`. The record is removed **only when the launcher
  returns**. A pane whose bash is killed first leaves it. There is no trap in the
  trampoline.

- **`ccy-sessions` has no stop subcommand** (dispatcher `ccy-sessions:75-85`: picker,
  `notify`, `reboot`, `restore`). Ctrl-X runs `kill-session` **and then deletes the record
  on purpose** (`:383-392`). That is the opposite of what this plan wants.

- Candidate stop mechanisms, **all UNVERIFIED for record survival** (the only tested kill
  is `kill -KILL` on the trampoline, `scripts/test-ccy-session-registry.bash:256-299`):

  - `tmux -L ccy kill-session -t =<name>` or `tmux -L ccy kill-server`. tmux hangs up the
    pane's pty, and the non-interactive bash running the trampoline should die on SIGHUP
    before it reaches `rm`. That is a signal-ordering argument, not a test.
  - `systemctl --user stop 'ccy-tmux-*.scope'`: SIGTERM to the cgroup, then SIGKILL. The
    tmux server lives in the scope of the **first** session started, because each later
    `systemd-run --scope` wraps only a short-lived client (`lib/tmux-session.bash:538-569`).
    Inferred, UNVERIFIED.
  - A reboot. This is the only case Plan 00135 designed for (`docs/ccy.md` "Sessions
    Survive a Reboot"). Its real-machine proof, Phase 5, is **still open**
    (`00135 PLAN.md:205-233`).

- **What happens to the container.** It runs `podman run … --rm --name <project>_yolo…`
  (`claude-yolo:3151-3152`, name from `get_next_container_name` `:2896`). The lib header
  says a SIGHUP'd podman client "runs its --rm teardown" (`lib/tmux-session.bash:5-8`).
  If the client is SIGKILLed first, the container can outlive it: **UNVERIFIED**.

- **Orphan risk is concrete, and it breaks unattended restore.** At every launch the
  launcher runs three checks (`claude-yolo:1361-1383`, `lib/docker-health.bash`):

  - `clean_stale_containers_startup` removes exited or created containers **without a
    prompt** (`docker-health.bash:288-330`).
  - `check_zombie_containers_startup` finds a **running** TTY container with no
    `podman run` process and shows an **interactive `[a/s/i/q]` menu**
    (`docker-health.bash:20-60,196-280`).
  - `check_project_containers_startup` finds **any running container for this project**
    and shows an **interactive `[c/s/m/q]` menu** (`docker-health.bash:500-580`).

  So: if a stopped session's container is still running, its restored pane waits at a
  prompt. The same happens when two sessions of one project are restored and one container
  comes up before the other launcher reaches its check (race, **UNVERIFIED**). The stop
  step must prove that no `label=ccy=true` container is left running before restore.

### (c) Restore

- `ccy-sessions restore [--dry-run]` → `ccy_registry_restore`
  (`session-registry.bash:339-419`). For each record: skip it when marked `no-restore`,
  skip it when the name is already live, and error when its directory is gone (the record
  is kept and the run continues). The status is non-zero if anything failed. It starts
  nothing if the live list cannot be read (`:370-373`).
- What is replayed: the recorded args minus the one-shot set (`ccy_registry_replay_args`
  `:83-166`). `ccy_registry_restore_args` (`:313-337`) then adds `--supervise` for `ccy`
  (unless the record already has `--supervise` or `--no-supervise`) and `--continue`
  (unless it already has `-c`, `--continue`, `-r` or `--resume`). `cc` gets `--continue`
  only.
- **A session whose launch prompted interactively prompts again, in the pane**
  (`docs/ccy.md` "Sessions Survive a Reboot": "one that answered prompts the first time
  asks them again, in the pane"). An unattended cycle would count it "restored" while it
  sits at a token, SSH or network prompt.
- **No TTY needed:** `ccy_tmux_start_detached` runs
  `systemd-run --user --scope … tmux -L ccy new-session -d … bash -c <trampoline>`
  (`lib/tmux-session.bash:544-569`). The pane gets its pty from tmux.
  `ccy-sessions-restore.service` is a `Type=oneshot` user unit that runs exactly this
  (`files/home/.config/systemd/user/ccy-sessions-restore.service:11-16`). The TTY guard
  applies to the picker only (`ccy-sessions:347-354`). The unit is **always deployed** and
  enabled only when `ccy_restore_sessions: true` (`play-claude-yolo.yml:707-826`), so
  `systemctl --user start ccy-sessions-restore` or a direct `ccy-sessions restore` works
  whether or not the host has opted in.
- **Environment caveat, UNVERIFIED:** a tmux server first started from a systemd user
  unit inherits that unit's minimal environment (no `SSH_AUTH_SOCK`; whatever PATH the
  unit has). Sessions started later on that server get it too.
- A base-image version change triggers a project-image rebuild inside the pane at launch
  (`claude-yolo:1668-1690`). It does not prompt, but it does slow the restart.

### Verifying a restored session actually resumed

No helper exists. Available signals (a combination is needed; each **UNVERIFIED** as a
proof of "resumed"):

- `tmux -L ccy list-sessions` / `list-panes -F '#{pane_dead} #{pane_current_command}'` (the
  library's `ccy_tmux_list` `lib/tmux-session.bash:45-56`).
- `tmux -L ccy capture-pane -p -t =<name>` to spot a prompt: `Choice [c/s/m/q]`, the
  zombie menu, a token prompt, or a `Press Enter to close` failure hold from the trampoline.
- `podman ps --filter label=ccy=true --filter label=ccy-project=<p>` (the labels are set at
  `claude-yolo:3153-3155`).
- The supervisor status file `<project>/.claude/hooks-daemon/untracked/supervise/supervisor-status.json`,
  holding `version`, `source_hash`, `pid` and `started_at`
  (`.claude/ccy/claude-supervise.py:223,266,2025-2062`). It is **per project, last writer
  wins**, not per session. The pid is a container pid.
- The project's hooks-daemon health (`bin/hooks-daemon status` inside the project) is a
  project-level fact, not proof about one session.

## 5. Existing server scheduling and unit conventions

- **The Plan 00109 server route** is `host-health-collect.timer` / `.service`, templated
  from `files/home/.config/systemd/user/host-health-collect.{timer,service}.j2`. It is
  deployed and enabled by
  `playbooks/imports/optional/common/play-host-health-login-report.yml:257-388`, only when
  `provisioning_profile == 'server'` (`:63`), and removed on desktop (`:173-181`).
  Conventions it establishes:
  - The timer uses `OnCalendar` + `OnStartupSec` + `RandomizedDelaySec` + `Persistent`
    (`timer.j2:15-28`).
  - The service is `Type=oneshot`, with `WorkingDirectory={{ root_dir }}`, an absolute
    `/usr/bin/python3 -m helpers…`, an explicit `TimeoutStartSec`, and
    `SuccessExitStatus=3` for "findings".
  - Reload **before** start and again **after** enable, because the systemd module reloads
    before it enables (`:351-388`). Read back the live graph rather than trusting
    `is-enabled` (`:196-253`).
  - That play has a **hard "reporting-only" boundary**: "nothing here re-runs a play … Re-running
    a play is always a human decision" (`play-host-health-login-report.yml:28-31`). Plan
    00137 would be the first automatic play runner, which contradicts that stated principle.
- Other units: user units live in `files/home/.config/systemd/user/` (`container-watch.timer`,
  `vmtest-nightly.timer`, `vmtest-bridge-heartbeat@.timer`, `ccy-sessions-restore.service`).
  System units live in `files/etc/systemd/system/` (`abrt-prune-stale.timer`, deployed by
  `play-basic-configs.yml`). Plain files are copied; `.j2` files are templated with
  `root_dir`. No path is hardcoded (`host-health-collect.service.j2:50-53`).
- Linger: `play-systemd-user-tweaks.yml:23-50` runs `loginctl enable-linger` and then
  explicitly starts `user@UID.service`, so the user manager is up at boot without a login.
  It is the single owner of that fact (`play-claude-yolo.yml:717-720`).
- The closest existing "update then cycle sessions" flow is `files/usr/local/bin/shutdown-with-update`,
  also linked as `reboot-with-update` by `play-basic-configs.yml:194-197`. It runs as root via
  sudo and needs `SUDO_USER` (`:109-126`). It rehearses
  `ccy-sessions notify going-down --dry-run` **before** updating (`:132`), then runs
  `dnf -y upgrade`, flatpak, pipx and rust (`:147-193`), then warns, counts down and reboots
  (`:247-267`), withdrawing the warning on any failure (`:30-60`). It updates **packages
  only** and neither pulls the repo nor runs plays.

## 6. Which plays are safe unattended while the sessions are down

There are no category or tag allowlists meant for this. The existing notions:

- **`scope: general | gnome | server`** in every play (`CLAUDE/AnsibleStyle.md:273-324`),
  QA-enforced. On a server, `gnome` plays `end_play` after the guard, so they are harmless
  no-ops. Core-play scopes (read from each file):
  - `gnome`: browsers, comms, firefox, gnome-shell, gnome-shell-extensions, gsettings,
    ms-fonts, terminal-emulators, toolbox-install, vscode.
  - `general`: AA-preflight-sanity, AB-dnf-upgrade, ZZ-repo-cleanup, basic-configs,
    claude-yolo, git-configure-and-tools, git-hooks-security, github-cli-multi,
    lxc-install-config, markless, mask-intel-lpmd, network-wait-tuning, nvm-install,
    podman, prevent-ssh-suspend, python, rpm-fusion, suspend-and-lid-policy,
    systemd-user-tweaks, tmux-sessions, vpn.
- **Tags are sparse and ad hoc** (for example `upgrade`/`dnf` in `play-AB-dnf-upgrade.yml:27`,
  and `always`/`systemd`/`linger` in `play-systemd-user-tweaks.yml:26-50`). They do not
  classify risk.
- The optional-play bundle `playbooks/imports/optional/server-recommended.bundle` is a list
  of plays for **provisioning**, not a safe-for-maintenance list (`run.bash:750-776`).

Plays that are dangerous or need care when run unattended (from reading them):

- `play-AB-dnf-upgrade.yml` runs `dnf upgrade name="*" state=latest` (`:72-75`). It can
  change the kernel ("KERNEL CHANGED — reboot", `:96`, `:245-264`) and upgrade podman or
  crun under the running containers. It is the first real play of `playbook-main.yml`
  (`playbook-main.yml:6`).
- `play-podman.yml` runs `podman system migrate` **only when no container is running**
  (`:106-145`). So with sessions down it **will** run, which is the intended repair. Its
  destructive reset is opt-in only (`podman_storage_reset`, `:73-104`).
- `play-suspend-and-lid-policy.yml` restarts `upower` and notifies "REBOOT REQUIRED" for
  logind (`:118-120,377-383,415-424`).
- DKMS and akmods: `play-displaylink.yml` is optional, under hardware-specific, and
  `shutdown-with-update:218-240` waits for akmods. It is not relevant to a server unless it
  was run there, so check the ledger.
- `play-lxc-install-config.yml` (NetworkManager handler, `:62-81`) and `play-vpn.yml`
  touch networking. **UNVERIFIED** whether either restarts networking on a no-change run.
- `play-claude-yolo.yml` replaces the launcher, lib, host `claude` and image. That is the
  reason for stopping sessions.

## 7. Concurrency and locking

- **Nothing prevents two Ansible runs at once.** There is no lock in `run.bash`, in the play
  shebang route, in `fedora-desktop-health --run-play`
  (`files/home/.local/bin/fedora-desktop-health.j2:103-111`), or in the plays.
- The ledger tolerates concurrency but does not prevent it. `O_APPEND` whole-line appends
  (`helpers/play_ledger/store.py:67-68`), an `O_EXCL` genesis write (`:42`), and a fold by
  `finished` timestamp rather than by position (`ledger.py:185-187`).
- dnf holds its own package lock. That is the only mutual exclusion in the stack.
- Existing lock conventions:
  - `CLAUDE/Plan/mkplan.bash:120-130`: an atomic `mkdir` lock with a bounded retry, and
    `die` naming the stale lock.
  - `helpers/displaylink_recovery/run_recovery.py:375-380,425-427`: `fcntl.flock`
    `LOCK_EX|LOCK_NB` on a lock file, which skips when it is held.
  - systemd itself will not start a second instance of a oneshot that is still activating.
    That covers the timer against itself, not the timer against a human.

## 8. Security

- The job runs as the user with `become`. `run.bash` refuses root (`:1161-1167`,
  `:267-270`). With NOPASSWD:ALL, or a sudo password persisted for the timer, that is
  **root-equivalent**. So a periodic pull-then-run means: **whoever can land a commit on the
  tracked branch, or in the checkout, gets root on the server at the next tick.**
- **No signature verification exists.** Recent F44 commits are unsigned (`git log --format=%G?` shows `N` on the last eight). Commit signing is only a TODO
  (`play-git-configure-and-tools.yml:12-14`), and Plan 00035 Phase 6 is unexecuted
  research. There is no `verify-commit` or `allowedSignersFile` anywhere.
- Branch protection: the `F*` branches are "PR-protected" but the **owner has push-bypass**
  (`docs/development.md:156-158`). CI (`.github/workflows/qa.yml`) runs on push but gates
  nothing that a bypassing push has not already landed. A self-updater could require green
  CI on the target commit (via `gh api` commit status) as a weaker gate. No code does this
  today.
- **The escalation paths come from the sessions themselves.**
  - ccy containers get `GH_TOKEN` (`claude-yolo:3165`) and mount `$PWD` at `/workspace`
    (`:2016`). This repo's own `CLAUDE.md` gives agents standing authority to push the
    current branch. A ccy agent with the owner's token can therefore push to F44, and the
    timer would then run that commit as root.
  - If a ccy session works **in the same checkout** the timer runs from, the container can
    commit there directly with no push at all. The pull then merges it, because
    `pull.rebase false` gives a merge rather than a refusal.
  - `cc` sessions run Claude on the host as the user already.
  - `core.hooksPath=scripts/git-hooks` is tracked content, so a pulled commit can add a
    `post-merge` hook that runs at pull time (there is none today: `scripts/git-hooks/`
    holds `commit-msg`, `pre-commit` and `lib`).
- **Vault password on a server:** the operator supplies it once as
  `RUN_BASH_VAULT_PASSWORD_FILE`, on tmpfs `/run/secrets`
  (`docs/headless-server-install.md:168-187`), and `run.bash` copies it to
  `~/Projects/fedora-desktop/<ansible.cfg vault_password_file>` at 0600
  (`run.bash:2658,662-664`). The copy **persists** there, which is what lets later plays
  start. The token, SSH passphrase and sudo password files do **not** persist: they live on
  tmpfs, and the doc chose that on purpose (`docs/headless-server-install.md:94-98`).

---

## Gaps to build

01. **A safe updater.** Fetch, then refuse on dirty, diverged, detached or wrong-branch
    state, then `merge --ff-only` to the new commit, then re-check the Fedora version. Keep
    its own old/new SHAs. Nothing reusable exists; `run.bash`'s bare `git pull` merges on
    divergence.
02. **"Which plays changed" by path.** `git diff --name-only OLD..NEW` mapped to plays
    (`src:` paths, `import_tasks`, `vars/`, `helpers/`). The ledger watches only play files
    and misses CCY launcher and lib changes. The `judged` API can be layered on for
    play-file changes, GONE and UNEXPLAINED. Failed last runs and never-run plays must be
    handled outside the ledger.
03. **An unattended single-play runner.** Sudo-only preflight, `--become-password-file`
    support, no prompts, an explicit PATH, and a non-zero exit on failure. Headless
    single-play is refused today (`run.bash:1125-1128`). The non-headless route works only
    on NOPASSWD, and by accident of EOF handling.
04. **A session stop that preserves records.** A `ccy-sessions` subcommand (for example
    `stop`) that kills sessions without `ccy_registry_remove`, then waits until
    `podman ps --filter label=ccy=true` is empty, and fails loudly if a container survives.
    SIGHUP/SIGTERM record survival needs a test; only SIGKILL is tested.
05. **Restore that cannot block on a prompt.** Unattended cycles need restores that cannot
    sit at the zombie or existing-container menus, or at token/network prompts. At minimum,
    a post-restore check (pane alive, container up, no prompt visible in `capture-pane`), and
    a loud report when it fails.
06. **A warning kind for "sessions restart, no reboot".** This is a hooks-daemon change on a
    public tracker, and the owner decides whether to file it. Until then the existing texts
    say "reboot".
07. **A lock** shared by the updater, `run.bash` single-play and `--run-play`. Use flock on a
    state-dir lock file, or the mkdir pattern.
08. **Trust gate before running anything:** signed-commit verification, or a pinned-signer
    `git verify-commit`, or at least green CI on the target SHA.
09. **The unit pair and its play.** A new `…timer`/`…service` under
    `files/home/.config/systemd/user/`, deployed server-only following the
    `play-host-health-login-report.yml` pattern (reloads split, live-graph readback). Where it
    lives in the IaC graph is an open question; see decision 7.
10. **Fetch auth on GitHub-configured servers.** Their origin is SSH with an agent-only
    key, so either use an HTTPS fetch URL for the public repo or accept that it cannot fetch.
11. **Overlap with host-health-collect's daily fetch.** Pick a non-overlapping schedule, or
    handle a ref-lock error explicitly.
12. **Plan 00135 Phase 5 (real-machine proof of record survival and restore)** is still
    open. This plan depends on it.

## Decisions the owner must make

1. **Restart mechanism.**
   - (a) Stop sessions, run plays, restore in place. No reboot and quicker, but it needs
     gaps 4 and 5 and an unproven kill path.
   - (b) Pull, run plays, then `systemctl reboot` with the existing
     `ccy-sessions notify going-down` and the boot-time restore. It reuses the one path
     Plan 00135 designed for and picks up kernel changes, but reboots on every update.
   - (c) Hybrid: reboot only when the kernel or other packages changed.
2. **Which plays are run.**
   - (a) Only `play-claude-yolo.yml` when its inputs changed. Smallest blast radius.
   - (b) An explicit allowlist file of plays that are safe unattended.
   - (c) Everything the path-diff maps to. Broad, and needs its own safeguards.
   - Whether `play-AB-dnf-upgrade.yml` is ever in scope. It is the riskiest play, and
     `shutdown-with-update` already covers package updates.
3. **Trust model.**
   - (a) Accept "push access = root" and document it.
   - (b) Require signed commits from a pinned key. Strongest, but needs signing set up,
     which is Plan 00035 Phase 6.
   - (c) Require green CI on the SHA. It stops an accident, not a malicious push.
   - (d) Follow a separate release ref that only the owner moves.
   - Also decide whether ccy tokens on the server may push to the tracked branch.
4. **Which checkout.** The same `~/Projects/fedora-desktop` the sessions may be developing
   in (a dirty or local-commit risk, and container-writable), or a separate deploy-only
   clone.
5. **Sudo for the timer.** NOPASSWD:ALL (simple, but permanent root for the user), or a
   sudo password persisted on disk for the timer. That reverses the tmpfs-only choice in
   `docs/headless-server-install.md:94-98`.
6. **Warning policy.**
   - How long agents get (N minutes). There is no acknowledgement channel.
   - Whether one project without a daemon CLI blocks the whole cycle (today's `notify`
     behaviour) or excludes only that session.
   - Whether to file the "sessions restart, no reboot" signal kind upstream (public
     tracker).
7. **The reporting-only boundary.** Plan 00109 states that re-running a play is always a
   human decision (`play-host-health-login-report.yml:28-31`). Either this plan explicitly
   supersedes that for servers, or the automation stays separate and lives in its own play.
8. **Failure behaviour.** If a play fails with sessions down, restore anyway on the old
   deployed state (half-applied), or leave them down and alert. And where the alert goes:
   the journal only, the login snippet via the host-health state, or elsewhere.
9. **Cadence and window.** For example nightly, away from 06:15, with
   `RandomizedDelaySec` and `Persistent`. And whether to skip when a human is logged in or
   a session is attached (`#{session_attached}` from `ccy_tmux_list`).
