# Inventory: host run-time execution from the live checkout

Read-only audit of the systemd units, timers and cron entries, shell snippets and
wrappers, symlinks, Go builds, GNOME extensions, desktop files, and the `scripts/`
entries that deployed things call.

The repo-root variable is **`root_dir`**, defined per play as
`{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`: the directory holding
the `ansible.cfg` the play was run with, which is the live checkout. No play uses
`playbook_dir`, `inventory_dir` or `lookup('env','PWD')`. One play hardcodes
`repo_path: $HOME/Projects/fedora-desktop` (git hooks, F11). In Go the root is
`main.RootDir`, set by ldflags, and only `ccy-teams` has it. Deployed files also carry it
in `REPO_PATH` (ftp-camera) and `VMTEST_CHECKOUT` (vmtest).

"Risk on a WIP merge into F44": the checkout is the working tree that agents and the
owner switch branches in and merge WIP into. Anything in it that runs at run time runs
whatever is checked out now, with no play run in between. That code may be unreviewed,
from another branch, or half-merged. A checkout that has moved breaks it too.

## Counts

| Class                 | Findings |
| --------------------- | -------- |
| RUNTIME-FROM-CHECKOUT | 7        |
| DEPLOYED-OK           | 12       |
| REPO-BY-DESIGN        | 6        |

Symlinks into the repo: **none**. GNOME extensions: **all copied**. Cron, anacron and
`/etc/cron.d`: **none in the repo**. Desktop files and autostart entries pointing into
the repo: **none**.

---

## RUNTIME-FROM-CHECKOUT (must move)

### R1. `host-health.service` (desktop) runs `helpers.host_health.login_report` from the checkout

- Unit template: `files/home/.config/systemd/user/host-health.service.j2:23-24`.
  It has `ExecStart=/usr/bin/python3 -m helpers.host_health.login_report` and
  `WorkingDirectory={{ root_dir }}`; the import works because cwd is on `sys.path` for `-m`.
- Deployed by `playbooks/imports/optional/common/play-host-health-login-report.yml:148-151`
  to `~/.config/systemd/user/host-health.service`. The unit is a copy; the code it runs is not.
- Who runs it: the systemd user manager, at the end of every graphical login.
- What it imports from the checkout: `login_report`, plus `check_pins`, `freshness`,
  `status_document`, `probe` and `self_update_check` (`helpers/host_health/login_report.py:540-570`).
  It also reads checkout data: `--repo-root` defaults to the tree the module was loaded
  from (`login_report.py:373-375`), and it reads `vars/version-pins.yml` and the playbook text.
- Risk: every login runs whatever `helpers/` is checked out. If a WIP merge breaks the
  import, the health report fails, and the panel reads that as a stale document.
- Note: the freshness and pin axes judge the checkout on purpose, so those DATA reads stay.
  Only the CODE should come from a deployed copy, for example
  `/usr/local/lib/ccy-helpers/helpers/host_health`, with `--repo-root` passed explicitly.

### R2. `host-health-collect.service` and `.timer` (server) do the same

- `files/home/.config/systemd/user/host-health-collect.service.j2:23-24` has the same
  `ExecStart` and `WorkingDirectory={{ root_dir }}`, with `--no-notify`.
- The timer, `host-health-collect.timer.j2:16`, fires daily at 06:15.
- Who runs it: the user manager, unattended, on server-profile hosts. The deploying play is
  the same as R1.
- Risk: as R1, but nobody is watching when it fails.

### R3. bashrc snippet `host-health-report.bash` runs Python from the checkout in every interactive shell

- `files/home/bashrc-includes/host-health-report.bash.j2:32-48`. It checks that
  `{{ root_dir }}/helpers/host_health/login_message.py` exists, then runs
  `PYTHONPATH="{{ root_dir }}" /usr/bin/python3 -P -m helpers.host_health.login_message --once-a-day`.
- Deployed by `play-host-health-login-report.yml:303-304` to `~/.bashrc-includes/host-health-report.bash`.
- Who runs it: every interactive shell, including every tmux pane and every SSH login.
- Risk: the widest blast radius in this report. A syntax or import error in
  `helpers/host_health/` on the checked-out branch puts a traceback in every new terminal.
  The snippet always returns 0, so a login is never lost.

### R4. `~/.local/bin/fedora-desktop-health` runs report code and plays from the checkout

- `files/home/.local/bin/fedora-desktop-health.j2:33` has `ROOT_DIR="{{ root_dir }}"`.
  It runs three things from the checkout:
  - `:168`, the default mode: `PYTHONPATH=$ROOT_DIR python3 -P -m helpers.host_health.login_message --on-demand`.
  - `:145`: `helpers.host_health.play_runner --repo-root $ROOT_DIR --validate <play>`, then
    it execs the play through its shebang, which goes to the checkout's `run.bash`.
  - `:130`: `$ROOT_DIR/run.bash --rerun`.
- Deployed by `play-host-health-login-report.yml:324-325`.
- Who runs it: the owner by hand, and the GNOME panel extension. The extension calls it in
  a terminal (`extensions/fedora-desktop@fedora-desktop/terminal.js`, `sections/plays.js:4-5`,
  `statusDocument.js:69` `ON_DEMAND_COMMAND`).
- Classification: the report path (`:168`) and the validator (`:145`) are
  RUNTIME-FROM-CHECKOUT. The `--rerun` and `--run-play` launches of `run.bash` or a play
  are REPO-BY-DESIGN, because plays run from the checkout by definition. They still go
  through a checkout-loaded validator.
- Risk: the panel's "Re-run a play…" item and the report text are WIP code once a WIP
  branch is checked out.

### R5. `ccy-teams` (Go) runs `helpers/agent_bus/declare.py` and reads host_vars from the checkout. The ccy launcher calls it on every session start that uses teams

- Build: `playbooks/imports/play-claude-yolo.yml:860-863` runs `tasks/build-go-command.yml`
  with `go_command_ldflags: "-X main.RootDir={{ root_dir }}"`. The binary goes to
  `~/.local/bin/ccy-teams`, a real copy of a binary.
- Config: `src-go/internal/teams/config/config.go:36-42` derives these from RootDir:
  - `HostVars = RootDir/environment/localhost/host_vars/localhost.yml`
  - `Play = RootDir/playbooks/imports/play-agent-bus.yml`
  - `ElementPlay = …/play-agent-bus-element.yml`
- Execution: `src-go/internal/declare/declare.go:65-78`, wired in
  `src-go/internal/teams/host/host.go:191`, runs
  `env PYTHONPATH=<RootDir> python3 -P -m helpers.agent_bus.declare <sub>`.
  `host.go:164-170` refuses to run when that file is missing ("checkout has gone").
- Declare is used by `board`, `choose`, `chooser`, `create`, `find`, `login`, `lxcadd`,
  `ops` and `picker`.
- Callers outside the repo:
  - `files/var/local/claude-yolo/claude-yolo:854` `ccy-teams choose-teams`
  - `:1248` `describe-seats`
  - `:1278` `record-seats`
  - `:3789` `internal restart-args`
  - `files/var/local/claude-yolo/lib/agent-bus-seats.bash:89` `describe-seats --block`
  - `lib/agent-bus-membership.bash:49` `internal membership-init`
- Who runs it: the deployed ccy launcher, at every `ccy --teams` start and restart, and
  the owner interactively.
- Risk: high. Starting any team session imports `declare.py` from whatever is checked out.
  A WIP change to `helpers/agent_bus/` changes team resolution for every session at once,
  with no play run. Running `apply` or `remove` executes the checkout's plays; that part
  is REPO-BY-DESIGN, and `ops.go:316` checks that the play lies under RootDir.
- Fix shape: deploy `helpers/agent_bus` (declare plus its imports) to a lib dir and point
  `PYTHONPATH` there. Pass `HostVars` and the plays explicitly. RootDir then remains only
  for "run a play".

### R6. `gh-<alias>-token-phpstorm` shell function runs `helpers.github_scopes.cli` from the checkout

- `playbooks/imports/play-github-cli-multi.yml:1269`, inside the `blockinfile` that writes
  `~/.bashrc-includes/gh-aliases.inc.bash` (`:864-866`). The function
  `_gh_phpstorm_token_report` (`:1242`), called from the per-alias function at `:1236`, runs
  `(cd "{{ root_dir }}" && python3 -m helpers.github_scopes.cli missing)`.
- Who runs it: the owner, interactively.
- Risk: the scope check loads WIP code, and it also reads `vars/github-required-scopes.yml`
  from the checkout.

### R7. `ftp-camera --pass` execs the checkout's `vault.bash`

- `/etc/ftp-camera/config` is written by
  `playbooks/imports/optional/common/play-ftp-camera.yml:246-266` with
  `REPO_PATH={{ root_dir | realpath }}`.
- `src-go/internal/ftpcamera/app.go:362-384`: `pass()` reads `REPO_PATH` and execs
  `$REPO_PATH/vault.bash get ftp_camera_password`. The exemption is recorded in
  `src-go/qa/rules/table.go:183-187`.
- Binary: built by `build-go-command.yml` (`play-ftp-camera.yml:234-235`) to
  `~/.local/bin/ftp-camera`, a real copy.
- Who runs it: the owner, via `ftp-camera --pass`.
- Risk: medium-low. `vault.bash` is tied to the checkout's vault file by nature. Still,
  the deployed binary execs a checkout script whose content follows the current branch.
  Options: deploy a copy of `vault.bash`, or classify it as REPO-BY-DESIGN because the
  vault only exists in the checkout.

---

## DEPLOYED-OK (real copies; listed so the next pass does not re-check them)

| #   | What                                                                                                                                            | Evidence                                                                                                                                                                                                                                                                                                                                                                               |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | System units in `files/etc/systemd/system/*` (abrt-prune, agent-bus-hs/health/backup, kernel-version-manager, lxc-docker-iptables, self-update) | Every `ExecStart` is under `/usr/local/{bin,sbin,lib}` or `/usr/bin`                                                                                                                                                                                                                                                                                                                   |
| D2  | `displaylink-dock-recovery.service`, plus the inline copy at `play-displaylink.yml:346-348`                                                     | `WorkingDirectory=/usr/local/lib/displaylink-recovery`; helpers copied to `play-displaylink.yml:297-314`                                                                                                                                                                                                                                                                               |
| D3  | User units: ccy-sessions-restore and set-going, container-watch, wsi-stream-server-at-login, vmtest-nightly, vmtest-bridge@ and heartbeat@      | `ExecStart=%h/.local/bin/<binary>`; container-watch and vmtest helpers live in `/usr/local/lib/ccy-helpers` (`play-container-watch.yml:41`, `play-vm-test-lab.yml:146`)                                                                                                                                                                                                                |
| D4  | `vmtest` (Go) and `VMTEST_CHECKOUT`                                                                                                             | `lab.env` (`play-vm-test-lab.yml:322`) names the checkout, but only as the DATA spool `<checkout>/untracked/vmtest-bridge` (`helpers/vmtest/spool.py`, `retention.py`, `verify_response.py`). Helpers run with `PYTHONPATH=<deployed helpersLib>` (`src-go/internal/vmtest/lab.go:205-216`). `vmtest-bridge@.path` watches the same spool. This couples data to the checkout, not code |
| D5  | Inline units in plays: ssh-suspend-guard, NM wait, ydotoold, rescrobbled, rclone mounts                                                         | All `ExecStart` targets are system or `%h/.local/bin` binaries                                                                                                                                                                                                                                                                                                                         |
| D6  | Go binaries built with `tasks/build-go-command.yml`: ccy-sessions, ccy-teams, ftp-camera, wsi, vmtest, watermark, lxcfreeze, podfreeze          | `go build` runs in `{{ root_dir }}/src-go` to `~/.cache/ccy-go-build/<name>`, then is copied to `~/.local/bin` or `/usr/local/bin` (`build-go-command.yml:62-102`). No `go run` and no binary inside the repo tree. ccy-teams and ftp-camera appear above only for what they call at run time                                                                                          |
| D7  | md-viewer                                                                                                                                       | `go install` of the pinned external module `github.com/LongTermSupport/md-viewer/cmd/md-viewer@<ver>` (`play-md-viewer.yml:100-106`); the desktop file has `Exec={{ md_viewer_bin }}`                                                                                                                                                                                                  |
| D8  | GNOME extensions                                                                                                                                | Copied: `play-gnome-shell-extensions.yml:125-127`, `play-fedora-desktop-panel.yml:87-88`, `play-speech-to-text.yml:345-364`, `play-remote-desktop-toggle.yml:106-107`. Run-time spawns target `~/.local/bin/*`, apart from the panel's handoff to R4                                                                                                                                   |
| D9  | `~/.local/bin` wrappers: container-watch, github-ssh-443, vmtest-bridge-watcher                                                                 | `PYTHONPATH=/usr/local/lib/ccy-helpers` or `$VMTEST_HELPERS_LIB`                                                                                                                                                                                                                                                                                                                       |
| D10 | agent-bus homeserver and `agent-bus.pyz`                                                                                                        | Built by `agent-bus-install software --source {{ root_dir }}` at play time (`play-agent-bus.yml:178-181`). At run time units use `/usr/local/lib/agent-bus/*` and `agent-bus-install backup-now`                                                                                                                                                                                       |
| D11 | Unattended self-update (`fedora-desktop-self-update.{service,timer}`)                                                                           | Runs from a separate root-owned clone, `/var/lib/fedora-desktop/deploy` (`play-self-update.yml:27, 256-266, 416`), checked for a signed HEAD before anything is imported (`files/usr/local/sbin/fedora-desktop-self-update:85-158`). Never the live checkout                                                                                                                           |
| D12 | Symlink tasks                                                                                                                                   | All five (`play-basic-configs.yml:357`, `play-docker-in-lxc-support.yml:109`, `play-photography.yml:242`, `play-network-tools.yml:68`, `play-cli-tools.yml:170`) link system paths to system or vendor paths; none points into the repo                                                                                                                                                |

---

## REPO-BY-DESIGN

| #   | What                                                                                                                                                                                                                                                                                                                                                                                                                   | Note                                                                                                                                                                                                                                                                                                                                             |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| B1  | Play-time `chdir: "{{ root_dir }}"` plus `python3 -m helpers.*`: play-python `:218, :268`, suspend-and-lid `:316, :376`, lxc `:180`, gnome-shell-extensions `:163, :220`, browsers `:103, :144`, agent-bus `:140-358`, github-cli-multi `:215, :453, :685, :756, :805`, claude-yolo `:384-396` (pingbus bundle), displaylink `:415, :432`, fedora-desktop-panel, unifi-\*, self-update, vm-test-lab, agent-bus-element | These run while the play runs, as intended                                                                                                                                                                                                                                                                                                       |
| B2  | `run.bash`, play shebangs, `meta-deploy.bash`, plan scripts, `tests/`, `scripts/qa-*`                                                                                                                                                                                                                                                                                                                                  | Human or QA entry points                                                                                                                                                                                                                                                                                                                         |
| B3  | Git hooks: `play-git-hooks-security.yml:49-54` sets `core.hooksPath` to `<repo>/scripts/git-hooks`                                                                                                                                                                                                                                                                                                                     | Run on every commit in the checkout, so they follow the branch, which is expected for a repo's own hooks. Note: `repo_path` is hardcoded to `$HOME/Projects/fedora-desktop` (`:10`) instead of `root_dir`, so the play configures the wrong tree, or fails, for any other checkout location. That is a separate defect, worth a line in the plan |
| B4  | `watchlist` (Go)                                                                                                                                                                                                                                                                                                                                                                                                       | Never deployed. It is run with `cd src-go && go run ./cmd/watchlist` by the `/watchlist` skill (`.claude/skills/watchlist/SKILL.md:19`). Agent tooling inside the repo                                                                                                                                                                           |
| B5  | `scripts/` referenced by deployed code                                                                                                                                                                                                                                                                                                                                                                                 | Only in printed remedies: `scripts/gh-account-setup.bash` (`helpers/github_signing/cli.py:108`, `play-github-cli-multi.yml:239`), `scripts/release.bash`, `scripts/make-playbooks-executable.bash`. No deployed unit, wrapper or extension executes a `scripts/` file                                                                            |
| B6  | `ccy-teams apply`/`remove` and the `fedora-desktop-health --rerun`/`--run-play` launches                                                                                                                                                                                                                                                                                                                               | Running a play from the checkout is the point; only the helper code around them (R4, R5) needs to move                                                                                                                                                                                                                                           |

---

## Suggested shape of the fix (for the plan)

- One deployed helper lib, `/usr/local/lib/ccy-helpers/helpers/` already exists for
  github443, containerwatch and vmtest. Add `host_health` (R1-R4), `agent_bus` declare
  and its imports (R5), and `github_scopes` (R6). Every caller then uses
  `PYTHONPATH=<lib> python3 -P -m …`, as the D9 wrappers already do.
- Keep the checkout path wherever the DATA is the checkout: the freshness and pin
  judgement, host_vars, the plays, the vmtest spool. Pass it as an explicit argument,
  never as a code root.
- The QA rule table already lists `cli.execVault` as an allowed program runner
  (`src-go/qa/rules/table.go:183-187`). A similar gate could deny `root_dir` in deployed
  unit `WorkingDirectory=` lines, `PYTHONPATH="{{ root_dir }}"` in templates under
  `files/`, and `cd "{{ root_dir }}"` inside `blockinfile` or `copy content:` blocks.
