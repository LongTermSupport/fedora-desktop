# Bash and Python inventory (read-only survey)

Method: `git ls-files` + shebang/extension detection, metrics by regex (`untracked/scratch/inv.py`, `thr.py`, `report.py`; data in `inv.json`). Function counts, interactivity (gum/fzf/`read -r|-p`), JSON/YAML (jq/yq/python heredoc/-c) and associative-array flags are regex heuristics; invoker column is "who mentions the basename" (ansible/unit/QA/other script/sourced), so it over-counts. The "tests" mapping by basename is too loose to publish per row; per-script test suites are named in the prose where known. The ccy launcher is assessed in `CLAUDE/Plan/00191-go-cli-tooling/ASSESS-ccy-launcher.md` and is only referenced here.

## 1. Totals

| Set                                                            | Files | Non-blank non-comment lines                                                                                                       |
| -------------------------------------------------------------- | ----- | --------------------------------------------------------------------------------------------------------------------------------- |
| Bash outside CLAUDE/Plan/                                      | 266   | ~58k                                                                                                                              |
| of which `scripts/test-*.bash` (test suites)                   | 74    | ~16k                                                                                                                              |
| of which non-test                                              | 192   | 42,084                                                                                                                            |
| of which QA gates + git hooks (`scripts/qa-*`, `git-hooks/`)   | 26    | ~5.5k with their tests                                                                                                            |
| Bash in CLAUDE/Plan/ (plan-local scaffolding, summarised only) | 183   | 27,346                                                                                                                            |
| `.j2` rendering bash                                           | 2     | `fedora-desktop-health.j2` (173 lines), `claude-yolo.bash.j2` (11)                                                                |
| Ansible `shell:` blocks > 40 lines                             | 0     | largest is 28 lines (`playbooks/imports/play-github-cli-multi.yml:512`); 62 blocks total, 1 over 25 lines. Not a porting concern. |
| Python outside CLAUDE/Plan/                                    | 322   | see section 5                                                                                                                     |

Families (non-plan, summed): ccy launcher + libs 19 files / 11.4k lines / 576 commits; ccy tests 37 / 11.4k; run.bash family 9 / 4.3k; QA 31 / 5.5k; vmtest 17 / 3.7k; freeze (podfreeze, lxcfreeze, freeze-common) 6 / 3.3k; wsi 6 / 1.8k; fedora-install 4 / 1.7k; rclone 5 / 1.2k; agent-bus installer 2 / 1.1k.

## 2. Largest non-test scripts (code lines / total / funcs / flags / churn commits)

I = interactive, J = JSON/YAML parsing, A = assoc arrays, L = sourced as library.

| Script                                           | Code | Total | Funcs | Flags   | Churn | Invoker                         | Class                                                                                    |
| ------------------------------------------------ | ---- | ----- | ----- | ------- | ----- | ------------------------------- | ---------------------------------------------------------------------------------------- |
| `run.bash` (bootstrap)                           | 2849 | 4027  | 67    | I J A   | 168   | user, ansible tests, QA         | B (see special cases)                                                                    |
| `files/var/local/claude-yolo/claude-yolo`        | 2834 | 4003  | 12    | I       | 285   | user                            | A (ASSESS-ccy-launcher.md)                                                               |
| `.claude/init.sh`                                | 1934 | 3570  | 55    | I J L   | 24    | hooks-daemon                    | B, vendored                                                                              |
| `files/home/.local/bin/ftp-camera`               | 1619 | 2570  | 33    | I       | 43    | user                            | A                                                                                        |
| `claude-yolo/lib/ssh-handling.bash`              | 1398 | 2045  | 40    | I J L   | 43    | sourced by launcher, entrypoint | A (with ccy)                                                                             |
| `claude-yolo/lib/token-management.bash`          | 1386 | 2048  | 36    | I J A L | 31    | sourced                         | A (with ccy)                                                                             |
| `files/home/.local/bin/vmtest`                   | 1231 | 1686  | 67    | I J     | 26    | user, ansible                   | A                                                                                        |
| `claude-yolo/lib/session-registry.bash`          | 1044 | 1415  | 39    | I J A L | 19    | sourced                         | A (with ccy; sessions already partly Go)                                                 |
| `files/home/.local/bin/wsi`                      | 1000 | 1431  | 26    | -       | 31    | user                            | A                                                                                        |
| `files/usr/local/sbin/agent-bus-install`         | 995  | 1181  | 62    | I J A   | 15    | root, systemd                   | A                                                                                        |
| `claude-yolo/lib/network-management.bash`        | 962  | 1319  | 30    | I J A L | 24    | sourced                         | A (with ccy)                                                                             |
| `fedora-install/setup-netinstall-boot.bash`      | 957  | 1292  | 30    | I       | 22    | user, rare                      | B                                                                                        |
| `scripts/qa-all.bash`                            | 852  | 1485  | 2     | I J     | 120   | user, CI                        | B (QA, see below)                                                                        |
| `claude-yolo/lib/common.bash`                    | 689  | 1100  | 21    | I A L   | 44    | sourced                         | A (with ccy)                                                                             |
| `files/var/local/docker-in-lxc`                  | 649  | 953   | 20    | I       | 12    | user                            | B                                                                                        |
| `claude-yolo/lib/dockerfile-custom.bash`         | 556  | 777   | 4     | I J L   | 8     | sourced                         | A (with ccy)                                                                             |
| `scripts/gh-account-setup.bash`                  | 546  | 731   | 19    | I J     | 20    | user, ansible                   | A                                                                                        |
| `scripts/qa-changed.bash`                        | 530  | 636   | 18    | I J A   | 5     | user                            | B (QA)                                                                                   |
| `files/home/.local/bin/podfreeze`                | 529  | 926   | 27    | I A     | 13    | user                            | A (freeze)                                                                               |
| `files/home/.local/bin/nord`                     | 505  | 713   | 18    | I       | 8     | user                            | B                                                                                        |
| `freeze/freeze-common.bash`                      | 470  | 824   | 19    | I L     | 5     | sourced                         | A (freeze)                                                                               |
| `files/home/.local/bin/lxcfreeze`                | 462  | 954   | 26    | I       | 7     | user                            | A (freeze)                                                                               |
| `files/home/.local/bin/reclaim`                  | 456  | 548   | 29    | I       | 5     | user                            | B                                                                                        |
| `claude-yolo/lib/docker-health.bash`             | 454  | 652   | 9     | I L     | 9     | sourced                         | A (with ccy)                                                                             |
| `fedora-install/push.bash`                       | 446  | 587   | 10    | I J A   | 3     | user                            | B                                                                                        |
| `claude-yolo/entrypoint.sh`                      | 443  | 757   | 5     | I J     | 58    | container entrypoint            | C-ish: must stay shell inside the image unless a static Go binary is baked in (decision) |
| `scripts/setup-rclone.bash`                      | 439  | 582   | 9     | I       | 17    | user                            | B                                                                                        |
| `scripts/nvidia-status.bash`                     | 430  | 537   | 4     | I       | 3     | user                            | B                                                                                        |
| `files/home/.local/bin/open`                     | 419  | 565   | 18    | I J     | 2     | user                            | B                                                                                        |
| `claude-yolo/lib/tmux-session.bash`              | 390  | 588   | 22    | I L     | 20    | sourced                         | A (with ccy; tmux-targets has a Go port seam)                                            |
| `.claude/skills/hooks-daemon/scripts/install.sh` | 385  | 559   | 21    | I       | 5     | vendored hooks-daemon           | C: vendored, do not port                                                                 |
| `scripts/collect-diagnostics.bash`               | 364  | 508   | 9     | I       | 2     | ansible                         | B                                                                                        |
| `scripts/qa-ansible.bash`                        | 350  | 615   | 1     | I J     | 16    | QA                              | B (QA)                                                                                   |
| `claude-yolo/lib/common-pure.bash`               | 324  | 575   | 15    | I L     | 26    | sourced                         | A (with ccy)                                                                             |
| `rclone-cache-status`                            | 323  | 452   | 7     | I       | 6     | user                            | B                                                                                        |
| `claude-yolo/lib/restart-request.bash`           | 318  | 444   | 8     | I J L   | 4     | sourced                         | A (with ccy)                                                                             |
| `files/usr/local/bin/watermark`                  | 310  | 403   | 6     | -       | 1     | ansible/user                    | B                                                                                        |
| `files/usr/local/bin/shutdown-with-update`       | 290  | 425   | 10    | I       | 16    | systemd/user                    | B                                                                                        |
| `scripts/git-hooks/pre-commit`                   | 279  | 468   | 0     | I       | 17    | git                             | B (hook, stays shell, see below)                                                         |

The remaining 150 non-test scripts are under 270 lines; full per-script table is in section 7.

## 3. Test suites (74 files, ~16k lines)

Tests for a bash program disappear with a port (replaced by `go test`). Largest: `test-ccy-teams-command` (1096), `test-ccy-sessions-reboot` (1021), `test-ccy-session-registry` (870), `test-agent-bus-install` (785), `test-self-update-cycle` (752), `test-podfreeze` (733), `test-freezelib` (703), `test-ccy-ssh-handling` (702), `test-planlib` (569), `test-ccy-git-signing` (561), `test-wsi-stop-grace` (557). They are exempt from the "no big bash" rule only while the thing they test is bash; a Go port retires its tests rather than porting them line for line.

## 4. Threshold analysis

Counts over the 192 non-test, non-plan bash programs (266 incl. tests). Plan scaffolding is out of scope.

| Candidate trigger ("must be Go if ...")                                                | Non-test flagged | All non-plan flagged |
| -------------------------------------------------------------------------------------- | ---------------- | -------------------- |
| code lines > 30                                                                        | 135              | 209                  |
| code lines > 60                                                                        | 114              | 183                  |
| code lines > 100                                                                       | 88               | 144                  |
| code lines > 200                                                                       | 50               | 86                   |
| functions >= 4                                                                         | 72               | 128                  |
| functions >= 8                                                                         | 43               | 74                   |
| interactive (gum/fzf/read)                                                             | 79               | 103                  |
| parses JSON/YAML                                                                       | 69               | 82                   |
| associative array                                                                      | 17               | 21                   |
| RULE-3: code > 200 or assoc array or funcs >= 12                                       | 58               | 95                   |
| RULE-1: code > 100 or funcs >= 8 or assoc or (interactive and > 50) or (json and > 40) | 108              | 165                  |
| RULE-2: code > 60 or funcs >= 4 or interactive or json or assoc                        | 148              | 217                  |

Observations:

- Almost every candidate flags a large majority; the bulk of the repo is already above "very small helper". A literal "bash only for very small helpers" rule is a rule for NEW code, not a retroactive mandate.
- 45 scripts have \<= 15 code lines (true glue); 192 - 114 = 78 non-test scripts are \<= 60 lines.

Recommendation (new-code rule, enforced by a QA check on added/modified lines, with a ratchet baseline file of existing offenders so old scripts do not fail until touched):

1. Hard limit: a bash program may not exceed 60 code lines (non-blank, non-comment) or 4 functions.
2. Regardless of size: no JSON/YAML parsing beyond a single `jq -r` field extraction, no associative arrays, no gum/fzf menus or multi-step prompts, no nested loops over parsed data. These go to Go (or an existing Python helper if Ansible-coupled).
3. Scripts over the limit already in the repo are grandfathered by baseline; touching one for more than a bugfix triggers the port.
4. Under this rule 114 of 192 non-test programs are over the line limit (flag "port when touched"); the 58 matched by RULE-3 (>200 lines, assoc arrays or >= 12 functions) are the "port as a programme" set. Churn-weighted, the active ones are the real queue (see section 6).

Special cases where bash is the right tool (exempt, mark with an exemption comment/inventory, not counted):

- Sourced into the user shell: `files/home/bashrc-includes/*`, `files/etc/profile.d/zz_lts-fedora-desktop.bash`, `claude-yolo.bash.j2`, `ps1-prompt`, `colours` (11 snippets, 224 lines). Only genuinely shell-env code; heavy logic they call should be Go binaries.
- Bootstrap before Go exists: `run.bash`, `scripts/setup.bash`, `fedora-install/` kickstart helpers, `vault.bash`. `run.bash` is the 2849-line outlier: the bootstrap core (install ansible/git/go toolchain, run the play) must stay bash, but its 67 functions mostly implement features (ssh-agent, gh scopes, config repo, health, reruns) that run after the toolchain exists. Programme: shrink run.bash to a bootstrap stub that builds/installs a Go `fedora-desktop` binary and delegates (decision needed on bootstrapping Go via play vs. prebuilt release binary).
- Container entrypoints and scripts that run inside images without Go (`entrypoint.sh`, systemd-sleep hook `resuspend-aborted-suspend`): stay shell unless a static binary is baked in.
- Git hooks (`scripts/git-hooks/pre-commit`, 279 lines): git requires an executable, but it can be a 3-line shim exec'ing a Go binary. Treat as B.
- Vendored third-party (`.claude/init.sh`, `.claude/skills/hooks-daemon/scripts/install.sh`, `.claude/hooks-daemon`): out of scope.
- Generated dotfiles (templates emitting bash into user dotfiles): stay bash, but the generating template must be tiny and call a Go binary.
- QA gates: recommended to count as tooling for the size rule but with the ratchet applied last. They run in CI and on dev hosts where Go is already required by `qa-go.bash`, they are the most-churned scripts (`qa-all` 120 commits), and `qa-changed.bash` already carries assoc arrays and JSON. Short single-purpose gates (qa-toolchain, qa-docs, qa-helper-tests; 60-100 lines, 0-5 funcs) are fine in bash. Port `qa-all.bash`, `qa-changed.bash`, `qa-ansible.bash` (orchestration with state and summaries) after the shared libs.

## 5. Python inventory (outside CLAUDE/Plan/): 322 files

Python is mostly stdlib-only helpers (`helpers/`, 133 non-test modules ~25k lines) with a mirrored `tests/helpers/` tree (stdlib unittest). Only three modules import ansible; those are STAYS PYTHON.

STAYS PYTHON (Ansible-coupled):

- `callback_plugins/play_ledger.py` (212 lines) imports ansible, a callback plugin.
- `helpers/play_ledger/*` (13 modules, ~1.4k lines) is its support library, loaded by the callback via a plugin-support shim; keep with the callback. `helpers/self_update/*` and `helpers/play_lock/lock.py` are called from plays; borderline-to-stays because every caller is an Ansible task or a play-adjacent bash `self-update` flow.
- `.claude/hooks/handlers/pre_tool_use/*` (hooks-daemon handlers, must be Python in the daemon's venv) and `.semgrep/*.py`.
- `files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py` (434 lines): plugin loaded by the supervisor, which is Python (`.claude/ccy/claude-supervise.py`, 9,890 lines, 15 commits; vendored/daemon-owned: stays).
- Tests importing ansible: `tests/helpers/play_ledger/test_source_position_against_real_ansible.py`, `.claude/hooks/.../tests/test_ansible_enforcement.py`.

The Go seam (urgent): `helpers/agent_bus/declare.py` (1011 lines, 12 commits) is exec'd by `src-go/internal/declare/declare.go:336` (`PYTHONPATH=<root> python3 -P -m helpers.agent_bus.declare <sub>`); `src-go/internal/teams/app.go:246` checks its existence. That is the only Go to Python seam. It owns host_vars editing. The sibling modules it imports (`registry.py`, `element.py`, `teamfile.py`, `render.py`, `checkout.py`, `admin.py`, `cli.py`) plus `helpers/agent_bus_lxc/members.py` (935) are the agent-bus library behind `agent-bus-install`; test mirror `tests/helpers/agent_bus/` (11 test files, ~5.5k lines).

PORT TO GO (standalone CLIs/daemons, no Ansible import), in rough priority:

1. agent_bus family: `declare.py` (the seam), `admin.py` 856, `checkout.py` 746, `members.py` 935, `cli.py`, `teamfile.py`, `element.py`, `registry.py`, `render.py` (~5.9k lines total). Removes the python3 exec from ccy-teams. Pairs with the `agent-bus-install` bash port.
2. pingbus (`helpers/pingbus/*`, 14 modules, ~7.0k lines, high churn 14-18 commits on cli/protocol/forge): a standalone message-bus CLI/daemon with an HTTP client, a natural Go fit; test tree is ~14k lines (plus fakes) and would be rewritten.
3. `helpers/containerwatch/*` (~2.2k lines, `cli.py` 1097, 13 commits): daemon-style watcher.
4. `files/home/.local/bin/wsi-stream` (1792, 48 commits), `wsi-stream-server` (1214, 20), `wsi-article*`, `wsi-model-manager`, `wsi-server-manager`, `wsi-resolve-model` (~4.4k lines): the speech-to-text stack. High churn but audio/ML ties (check whether it calls Python-only libs before deciding).
5. `files/home/.local/bin/clip-scan` (742, 5 commits), `git-account-helper.j2` (257, template), `helpers/vmtest/*` (30 modules, ~5.5k lines; low churn, pairs with the vmtest bash port), `helpers/docs/link_check.py` (493, 11 commits), `helpers/version_pins/*` (14 commits on `check_pins`; QA-adjacent).

BORDERLINE: `helpers/host_health/*` (login report 21 commits; writes status documents consumed by a bash template), `helpers/self_update/*` (cycle.py 974, 13 commits; coupled to plays), `helpers/gnome/*` (talks to dconf/DBus; Ansible-invoked, small), `helpers/unifi/*`, `helpers/displaylink_recovery/*`, `helpers/github_signing|github_scopes|github443/*`, `helpers/qa_environment/*`, `helpers/rpm_keys`, `helpers/ssh_agent_filter` (a daemon: port candidate), `scripts/config_merge.py`.

Everything else small (`sshd_ports`, `suspend_wakeup`, `pyenv`, `dconf`) stays Python: Ansible-invoked, tiny.

The Python rule should be the same shape as the bash rule: new standalone CLIs are Go; Python only where Ansible or the hooks daemon loads it in-process.

## 6. Proposed rewrite programmes (category A; each a candidate feature branch)

Ordered by value (churn, size, interactivity) and dependency (libs first).

| #   | Programme                                                                                                                | Contents                                                                                                                                                                                                                   | Size               | Churn                                                                                                                                                  | Notes                                                                                                                                                                |
| --- | ------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1  | ccy launcher + libs                                                                                                      | `claude-yolo` + `lib/*.bash` (common, common-pure, ssh-handling, token-management, session-registry, network-management, tmux-session, docker-health, restart-request, dockerfile-custom) ~11.4k lines + ~11.4k test lines | largest            | 576 commits summed                                                                                                                                     | Already assessed in ASSESS-ccy-launcher.md. Libs are sourced so must move together with the launcher; sessions/teams already ported. Entrypoint stays shell.         |
| P2  | agent-bus (python + bash)                                                                                                | `helpers/agent_bus/*`, `agent_bus_lxc/members.py`, `agent-bus-install` (995 lines bash, 62 funcs, assoc arrays), `agent-bus-seats`                                                                                         | ~7k                | moderate                                                                                                                                               | Eliminates the only Go-to-Python seam; install script is interactive and JSON-heavy. Do first among non-ccy: highest bug surface per line, smallest dependency tail. |
| P3  | Shared QA/plan libs (`_planlib.inc.bash`, `qa-helper-summary`) and QA orchestrators `qa-all`, `qa-changed`, `qa-ansible` | ~2k lines                                                                                                                                                                                                                  | 120 + 5 + 16       | most-churned scripts; Go already a QA prerequisite (`qa-go.bash`). Do after P1/P2 prove the shared Go packages (execx, tmux, gum-replacement prompts). |                                                                                                                                                                      |
| P4  | run.bash shrink                                                                                                          | split features out to Go subcommands, leave bootstrap stub                                                                                                                                                                 | 2.8k               | 168 commits                                                                                                                                            | Needs bootstrap decision (how Go gets on a fresh host); ties with P6 gh-account/ssh-agent.                                                                           |
| P5  | vmtest                                                                                                                   | `vmtest` (1231, 67 funcs, I J), guest scripts, `helpers/vmtest/*` Python (30 modules)                                                                                                                                      | ~5k bash + 5.5k py | 26 +                                                                                                                                                   | Port bash and python together; test suites ~2.5k lines.                                                                                                              |
| P6  | Interactive account/setup tools                                                                                          | `gh-account-setup.bash` 546, `setup-rclone` 439, `reclaim` 456, `nord` 505, `rclone-cache-*`, `open` 419, `ssh-with-key` family, `nvidia-status`                                                                           | ~3.5k              | low                                                                                                                                                    | Good Go candidates because of prompts; low churn so lower priority.                                                                                                  |
| P7  | freeze toolkit                                                                                                           | `podfreeze`, `lxcfreeze`, `freeze-common.bash` + 3 tests (~3.3k + 2.5k test)                                                                                                                                               | ~1.3k code         | 38                                                                                                                                                     | Self-contained with its own library; clean port.                                                                                                                     |
| P8  | Media/UX tools                                                                                                           | `ftp-camera` 1619 (43 commits, interactive), `wsi` 1000 (31), `watermark`, wsi python stack                                                                                                                                | ~5k+               | high                                                                                                                                                   | ftp-camera is the best single interactive-tool candidate outside ccy; wsi stack waits on the ML question.                                                            |
| P9  | Install tooling                                                                                                          | `fedora-install/*` (1.7k)                                                                                                                                                                                                  |                    | 40                                                                                                                                                     | Runs before anything exists: keep bash unless the ISO build host already has Go. Low priority.                                                                       |
| P10 | Python standalone: pingbus, containerwatch, ssh_agent_filter, link_check, version_pins                                   | see section 5                                                                                                                                                                                                              | ~12k + tests       | pingbus high                                                                                                                                           | After P2.                                                                                                                                                            |

Dependency order: shared Go packages (prompts, exec, JSON, tmux) -> P2 (removes Python seam, exercises prompts/JSON) -> P1 -> P3 -> P5/P7 -> P4 -> P6/P8 -> P9/P10.

## 7. Per-script table

Appended below by script (generated, non-plan, sorted by code lines). Columns: path, code, total, funcs, I/J/A flags, churn, sourced-as-library, invokers (non-QA).

| path | code | total | funcs | flags | churn | lib | invokers |
|---|---|---|---|---|---|---|---|
| run.bash | 2849 | 4027 | 67 | IJA | 168 |  | ansible:fedora-desktop-health.j2,ansible:git-signing.yml,ansible:github-required |
| files/var/local/claude-yolo/claude-yolo | 2834 | 4003 | 12 | I | 285 | L | ansible:bash-conventions.yml,ansible:ccy-sessions-set-going.service.j2,ansible:c |
| .claude/init.sh | 1934 | 3570 | 55 | IJ | 24 | L | ansible:bash-conventions.yml,script:daemon-cli.sh,script:install.sh,sourced:prov |
| files/home/.local/bin/ftp-camera | 1619 | 2570 | 33 | I | 43 |  | ansible:play-ftp-camera.yml,ansible:play-photography.yml,ansible:play-rclone.yml |
| files/var/local/claude-yolo/lib/ssh-handling.bash | 1398 | 2045 | 40 | IJ | 43 | L | ansible:play-claude-yolo.yml,script:entrypoint.sh,script:restart-request.bash,so |
| files/var/local/claude-yolo/lib/token-management.bash | 1386 | 2048 | 36 | IJA | 31 | L | ansible:play-claude-yolo.yml,sourced:test-ccy-token-import-cleanup.bash,sourced: |
| files/home/.local/bin/vmtest | 1231 | 1686 | 67 | IJ | 26 |  | ansible:gnome-shell-extensions.yml,ansible:play-vm-test-lab.yml,ansible:vm-test- |
| scripts/test-ccy-teams-command.bash | 1096 | 1269 | 17 | IJ | 20 |  |  |
| files/var/local/claude-yolo/lib/session-registry.bash | 1044 | 1415 | 39 | IJA | 19 | L | ansible:play-claude-yolo.yml,script:network-management.bash,script:restart-reque |
| scripts/test-ccy-sessions-reboot.bash | 1021 | 1259 | 15 |  | 23 |  |  |
| files/home/.local/bin/wsi | 1000 | 1431 | 26 |  | 31 |  | ansible:play-claude-yolo.yml,ansible:play-fast-file-manager.yml,ansible:play-net |
| files/usr/local/sbin/agent-bus-install | 995 | 1181 | 62 | IJA | 15 |  | ansible:agent-bus-backup@.service,ansible:agent-bus-hs@.service,ansible:play-age |
| files/var/local/claude-yolo/lib/network-management.bash | 962 | 1319 | 30 | IJA | 24 | L | ansible:play-claude-yolo.yml,sourced:test-ccy-compose-restore.bash,sourced:test- |
| fedora-install/setup-netinstall-boot.bash | 957 | 1292 | 30 | I | 22 |  |  |
| scripts/test-ccy-session-registry.bash | 870 | 1076 | 11 | IJA | 19 |  | script:session-registry.bash |
| scripts/qa-all.bash | 852 | 1485 | 2 | IJ | 120 |  | ansible:qa.yml,script:qa-helper-summary.bash |
| scripts/test-agent-bus-install.bash | 785 | 878 | 16 | IJ | 12 |  |  |
| scripts/test-self-update-cycle.bash | 752 | 939 | 19 | IA | 17 |  |  |
| scripts/test-podfreeze.bash | 733 | 1108 | 10 |  | 2 |  | script:podfreeze |
| scripts/test-freezelib.bash | 703 | 1040 | 26 |  | 5 |  | script:podfreeze |
| scripts/test-ccy-ssh-handling.bash | 702 | 875 | 19 |  | 11 |  |  |
| files/var/local/claude-yolo/lib/common.bash | 689 | 1100 | 21 | IA | 44 | L | ansible:bash-conventions.yml,ansible:deploy-freeze-lib.yml,ansible:play-claude-y |
| files/var/local/docker-in-lxc | 649 | 953 | 20 | I | 12 |  | ansible:bash-conventions.yml,ansible:play-docker-in-lxc-support.yml,script:bash- |
| scripts/test-planlib.bash | 569 | 821 | 13 | I | 9 |  |  |
| scripts/test-ccy-git-signing.bash | 561 | 673 | 12 |  | 13 |  |  |
| scripts/test-wsi-stop-grace.bash | 557 | 734 | 11 | IJ | 20 |  |  |
| files/var/local/claude-yolo/lib/dockerfile-custom.bash | 556 | 777 | 4 | IJ | 8 |  | ansible:play-claude-yolo.yml |
| scripts/gh-account-setup.bash | 546 | 731 | 19 | IJ | 20 |  | ansible:github-required-scopes.yml,ansible:play-github-cli-multi.yml,script:gh-p |
| scripts/qa-changed.bash | 530 | 636 | 18 | IJA | 5 |  |  |
| files/home/.local/bin/podfreeze | 529 | 926 | 27 | IA | 13 |  | ansible:deploy-freeze-lib.yml,ansible:play-lxcfreeze.yml,ansible:play-podfreeze. |
| scripts/test-ccy-network-disconnect.bash | 509 | 642 | 12 | IA | 4 |  |  |
| scripts/test-ccy-restart-request.bash | 508 | 652 | 11 | I | 7 |  |  |
| files/home/.local/bin/nord | 505 | 713 | 18 | I | 8 |  | ansible:play-nordvpn-openvpn.yml |
| scripts/test-ccy-teams.bash | 483 | 585 | 26 | I | 6 |  |  |
| files/home/.local/lib/freeze/freeze-common.bash | 470 | 824 | 19 | I | 5 | L | ansible:deploy-freeze-lib.yml,script:lxcfreeze,script:podfreeze,sourced:play-lxc |
| files/home/.local/bin/lxcfreeze | 462 | 954 | 26 | I | 7 |  | ansible:deploy-freeze-lib.yml,ansible:play-lxcfreeze.yml,ansible:play-podfreeze. |
| files/home/.local/bin/reclaim | 456 | 548 | 29 | I | 5 |  | ansible:play-cli-tools.yml,ansible:play-rclone.yml,ansible:play-systemd-user-twe |
| files/var/local/claude-yolo/lib/docker-health.bash | 454 | 652 | 9 | I | 9 | L | ansible:play-claude-yolo.yml,sourced:test-ccy-container-menus.bash |
| scripts/test-qa-helper-summary.bash | 452 | 718 | 7 | IJA | 19 |  | script:qa-helper-summary.bash |
| fedora-install/push.bash | 446 | 587 | 10 | IJA | 3 |  | script:pull-projects.bash |
| files/var/local/claude-yolo/entrypoint.sh | 443 | 757 | 5 | IJ | 58 |  | ansible:play-claude-yolo.yml,ansible:play-terminal-emulators.yml,script:ccy-clau |
| scripts/setup-rclone.bash | 439 | 582 | 9 | I | 17 |  |  |
| scripts/test-lxcfreeze.bash | 434 | 737 | 11 |  | 6 |  | script:lxcfreeze |
| scripts/nvidia-status.bash | 430 | 537 | 4 | I | 3 |  | ansible:play-nvidia.yml |
| files/home/.local/bin/open | 419 | 565 | 18 | IJ | 2 |  | ansible:deploy-freeze-lib.yml,ansible:fedora-desktop-health.j2,ansible:host-heal |
| scripts/test-ccy-lifecycle.bash | 416 | 511 | 14 |  | 6 |  |  |
| scripts/test-ccy-restore-askpass.bash | 415 | 521 | 11 |  | 11 |  |  |
| files/var/local/claude-yolo/lib/tmux-session.bash | 390 | 588 | 22 | I | 20 | L | ansible:play-claude-yolo.yml,script:network-management.bash,script:session-regis |
| scripts/test-vmtest-kernel-selection.bash | 389 | 611 | 9 | I | 7 |  | script:guest-prepare-server-host-health-kernel-change.bash |
| scripts/test-ccy-token-usage-command.bash | 388 | 454 | 12 | I | 4 |  |  |
| .claude/skills/hooks-daemon/scripts/install.sh | 385 | 559 | 21 | I | 5 |  | ansible:play-claude-yolo.yml,ansible:play-nvm-install.yml,script:init.sh,script: |
| scripts/collect-diagnostics.bash | 364 | 508 | 9 | I | 2 |  | ansible:play-collect-diagnostics.yml |
| scripts/qa-ansible.bash | 350 | 615 | 1 | IJ | 16 |  | ansible:play-host-health-login-report.yml |
| scripts/test-vmtest-prepare-record.bash | 334 | 493 | 7 | IJ | 4 |  |  |
| scripts/test-ccy-relabel-preflight.bash | 331 | 407 | 13 |  | 4 |  | ansible:play-python.yml,script:common-pure.bash |
| scripts/test-ccy-sessions-take-over.bash | 329 | 438 | 9 | I | 8 |  |  |
| scripts/test-agent-browser-session-guard.bash | 328 | 385 | 8 | J | 2 |  |  |
| files/var/local/claude-yolo/lib/common-pure.bash | 324 | 575 | 15 | I | 15 | L | ansible:play-claude-yolo.yml,script:agent-bus-seats.bash,script:cc-teams.bash,sc |
| scripts/test-release.bash | 324 | 407 | 14 |  | 2 |  |  |
| files/home/.local/bin/rclone-cache-status | 323 | 452 | 7 | I | 6 |  | ansible:play-rclone.yml,script:rclone-cache-warm,script:rclone-rc-auth.bash,scri |
| scripts/test-ccy-compose-restore.bash | 320 | 401 | 13 | I | 3 |  |  |
| files/var/local/claude-yolo/lib/restart-request.bash | 318 | 444 | 8 | IJ | 4 | L | ansible:play-claude-yolo.yml,script:session-lifecycle.bash,sourced:test-ccy-life |
| scripts/test-run-bash-gh-scopes.bash | 311 | 376 | 8 |  | 6 |  |  |
| files/usr/local/bin/watermark | 310 | 403 | 6 |  | 1 |  | ansible:play-image-watermarking.yml |
| files/usr/local/bin/shutdown-with-update | 290 | 425 | 10 | I | 16 |  | ansible:play-basic-configs.yml,ansible:play-claude-yolo.yml,ansible:play-python. |
| scripts/git-hooks/pre-commit | 279 | 468 | 0 | I | 17 |  | ansible:gnome-shell-extensions.yml,ansible:play-git-hooks-security.yml,ansible:p |
| scripts/test-fedora-desktop-health.bash | 278 | 377 | 5 | J | 3 |  |  |
| scripts/qa-nokill-containerwatch.bash | 275 | 449 | 7 | I | 5 |  | ansible:play-container-watch.yml |
| scripts/test-ccy-agent-bus.bash | 271 | 355 | 9 |  | 5 |  | script:entrypoint.sh |
| files/home/.local/share/vmtest/guest-acceptance-server-host-health-kernel-change.bash | 265 | 454 | 7 | J | 3 |  | ansible:vm-test-scenarios.yml |
| scripts/lint | 257 | 372 | 6 | J | 6 |  | ansible:.golangci.yml,ansible:play-golang.yml,ansible:play-python.yml,ansible:qa |
| scripts/test-git-signing-declared.bash | 253 | 349 | 7 |  | 2 |  |  |
| scripts/test-ccy-tmux-targets.bash | 246 | 319 | 4 | I | 6 |  |  |
| scripts/mark-branches-superseded.bash | 236 | 355 | 12 |  | 2 |  |  |
| files/home/.local/bin/ssh-keys-rekey | 228 | 332 | 5 | I | 4 |  | ansible:play-basic-configs.yml |
| scripts/test-run-bash-single-play.bash | 228 | 286 | 7 | IJ | 6 |  |  |
| scripts/release.bash | 227 | 295 | 8 | I | 2 |  |  |
| .claude/skills/hooks-daemon/scripts/optimise-invoke.sh | 226 | 348 | 0 |  | 1 |  |  |
| fedora-install/pull-projects.bash | 226 | 313 | 5 | IJ | 12 |  | script:push.bash,script:run.bash |
| files/home/.local/bin/rclone-tail | 226 | 331 | 7 | IA | 7 |  | ansible:play-rclone.yml,script:ftp-camera,script:rclone-cache-status,script:rclo |
| scripts/test-vmtest-member.bash | 222 | 275 | 16 |  | 3 |  |  |
| scripts/test-run-bash-config-repo.bash | 217 | 274 | 11 | J | 3 |  |  |
| scripts/git-hooks/lib/secret-scan.bash | 216 | 357 | 6 | IA | 4 | L | ansible:gnome-shell-extensions.yml,ansible:play-git-hooks-security.yml,ansible:p |
| scripts/test-cc-teams.bash | 216 | 270 | 2 | IJ | 3 |  |  |
| scripts/test-run-bash-rerun.bash | 215 | 260 | 7 |  | 1 |  |  |
| scripts/check-displaylink-status.sh | 211 | 271 | 1 | I | 5 |  | ansible:play-displaylink.yml |
| files/home/.local/share/vmtest/guest-acceptance-desktop.bash | 199 | 266 | 2 | J | 5 |  | ansible:gnome-shell-extensions.yml,ansible:vm-test-scenarios.yml |
| scripts/qa-patterns.bash | 196 | 365 | 0 | IJA | 9 |  | script:bash-conventions.bash |
| files/home/.local/bin/lan-scan | 190 | 250 | 10 | I | 5 |  | ansible:play-network-tools.yml,script:qnap-finder |
| scripts/test-bash-history-search.bash | 189 | 255 | 3 | I | 5 |  |  |
| files/var/local/claude-yolo/lib/session-lifecycle.bash | 185 | 263 | 8 |  | 3 | L | ansible:play-claude-yolo.yml,script:entrypoint.sh,sourced:test-ccy-lifecycle.bas |
| scripts/qa-discovery.bash | 185 | 395 | 14 | I | 13 | L | sourced:qa-ansible-syntax.bash,sourced:qa-bash.bash,sourced:qa-changed.bash,sour |
| files/home/.local/share/vmtest/guest-acceptance-server-github-token.bash | 184 | 294 | 3 | I | 1 |  | ansible:vm-test-scenarios.yml |
| files/var/local/claude-code/cc | 183 | 383 | 2 | IJ | 14 | L | ansible:.golangci.yml,ansible:agent-bus-hs@.service,ansible:agent-bus-read-only. |
| scripts/test-run-bash-changed.bash | 183 | 226 | 6 |  | 3 |  |  |
| .claude/skills/hooks-daemon/scripts/health-check.sh | 182 | 280 | 3 |  | 5 |  | script:_locate-cli.sh,script:_resolve-venv.sh,script:daemon-cli.sh,script:init-h |
| vault.bash | 179 | 242 | 11 | IJ | 7 |  | ansible:play-claude-yolo.yml,ansible:play-ftp-camera.yml,ansible:play-github-cli |
| scripts/test-ccy-token-mode.bash | 172 | 294 | 4 | I | 5 |  |  |
| files/var/local/claude-code/cc-teams.bash | 170 | 226 | 9 | IJ | 3 |  | ansible:play-claude-yolo.yml |
| scripts/test-qa-changed.bash | 170 | 222 | 5 |  | 6 |  |  |
| files/home/.local/share/vmtest/guest-prepare-server-host-health-kernel-change.bash | 169 | 355 | 7 | IJ | 9 |  | script:guest-acceptance-server-host-health-kernel-change.bash |
| scripts/test-host-health-login-snippet.bash | 169 | 300 | 5 |  | 4 |  | ansible:host-health-report.bash.j2,script:guest-prepare-server-host-health-kerne |
| scripts/qa-deployed-drift.bash | 166 | 311 | 1 | I | 12 |  | ansible:play-vm-test-lab.yml |
| scripts/test-vmtest-host-only-gate.bash | 162 | 264 | 4 |  | 1 |  | script:vmtest |
| scripts/test-vmtest-reboot-dispatch.bash | 159 | 247 | 12 |  | 3 |  |  |
| scripts/qa-python.bash | 155 | 271 | 0 | JA | 15 |  |  |
| scripts/test-ccy-token-usage-output.bash | 154 | 197 | 8 | I | 1 |  |  |
| scripts/test-qa-tool-pin-converges.bash | 151 | 214 | 4 |  | 1 |  |  |
| scripts/test-gh-alias-wrappers.bash | 150 | 203 | 6 | J | 2 |  |  |
| scripts/test-ccy-token-import-cleanup.bash | 149 | 191 | 8 |  | 1 |  |  |
| scripts/test-secret-scan.bash | 149 | 283 | 4 |  | 8 |  |  |
| .claude/provision.sh | 148 | 243 | 4 |  | 1 |  | script:init.sh |
| files/var/local/claude-yolo/lib/agent-bus-seats.bash | 145 | 207 | 7 | I | 2 | L | ansible:play-claude-yolo.yml,sourced:test-ccy-teams.bash |
| files/home/.local/bin/ccdt | 144 | 243 | 6 |  | 6 |  | ansible:play-claude-devtools.yml,script:claude-devtools.bash |
| .claude/skills/hooks-daemon/scripts/init-handlers.sh | 142 | 223 | 2 |  | 5 |  | script:_locate-cli.sh,script:_resolve-venv.sh,script:daemon-cli.sh,script:health |
| files/home/.local/share/vmtest/guest-acceptance-server.bash | 138 | 199 | 2 |  | 1 |  | ansible:vm-test-scenarios.yml,script:guest-acceptance-desktop.bash,script:guest- |
| scripts/check-pinned-versions.bash | 137 | 207 | 3 | IJ | 4 |  | ansible:version-pins.yml |
| files/home/.local/bin/qnap-finder | 135 | 183 | 4 | I | 2 |  | ansible:play-network-tools.yml |
| files/home/.local/bin/wsi-claude-process | 125 | 187 | 3 |  | 5 |  | ansible:play-speech-to-text.yml,script:wsi,script:wsi-article-window,script:wsi- |
| scripts/qa-vmtest-manifest.bash | 125 | 192 | 4 | J | 3 |  | ansible:vm-test-scenarios.yml |
| files/var/local/claude-yolo/agent-browser-session-guard | 123 | 173 | 2 | J | 3 |  | ansible:play-claude-yolo.yml |
| scripts/test-run-bash-headless-localhost-yml.bash | 122 | 183 | 5 |  | 4 |  | script:run-log-scrub.bash |
| scripts/test-run-log-scrub.bash | 122 | 210 | 4 |  | 2 |  |  |
| files/home/.local/bin/raw-prune | 119 | 163 | 1 | IA | 2 |  | ansible:play-photography.yml,script:clip-scan |
| files/usr/local/bin/agent-bus | 119 | 172 | 9 |  | 6 | L | ansible:.golangci.yml,ansible:agent-bus-backup@.service,ansible:agent-bus-hs@.se |
| .claude/skills/hooks-daemon/scripts/daemon-cli.sh | 118 | 204 | 2 |  | 5 |  | script:_locate-cli.sh,script:_resolve-venv.sh,script:health-check.sh,script:init |
| scripts/lib/qa-helper-summary.bash | 118 | 286 | 3 | I | 12 | L | sourced:qa-all.bash,sourced:qa-changed.bash |
| scripts/git-hooks/commit-msg | 117 | 193 | 0 | I | 6 |  | ansible:play-git-hooks-security.yml,script:secret-scan.bash |
| scripts/lib/run-log-scrub.bash | 117 | 229 | 4 | I | 2 |  | ansible:play-vm-test-lab.yml,script:vmtest |
| scripts/test-bash-history-session.bash | 117 | 165 | 5 | I | 2 |  |  |
| scripts/test-ccy-env-local-dist.bash | 115 | 158 | 5 |  | 4 |  |  |
| scripts/qa-version-pins.bash | 111 | 192 | 4 | IJ | 3 |  | ansible:version-pins.yml,script:check-pinned-versions.bash |
| scripts/qa-bash.bash | 110 | 202 | 0 | JA | 11 |  | script:bash-conventions.bash |
| files/home/.local/bin/ssh-agent-start | 109 | 131 | 6 | I | 1 |  | ansible:play-basic-configs.yml,ansible:play-git-configure-and-tools.yml |
| files/home/.local/bin/fedora-desktop-health.j2 | 108 | 173 | 2 | I | 3 |  | ansible:play-host-health-login-report.yml |
| files/home/.local/bin/ssh-with-agent.bash | 108 | 157 | 6 | I | 2 |  | ansible:play-basic-configs.yml,script:scp-with-key,script:scp-with-password,scri |
| files/usr/local/bin/uncompress | 106 | 137 | 2 |  | 2 |  | ansible:play-cli-tools.yml |
| scripts/test-ccy-container-version-hook.bash | 106 | 145 | 6 |  | 3 |  |  |
| scripts/qa-speech-to-text-rules.bash | 105 | 140 | 1 | IJ | 1 |  |  |
| scripts/test-ccy-host-time-zone.bash | 104 | 153 | 3 |  | 2 |  | script:common-pure.bash |
| scripts/test-ccy-ssh-probe.bash | 104 | 133 | 3 | I | 1 |  |  |
| scripts/qa-js.bash | 102 | 167 | 0 | IJ | 9 |  |  |
| scripts/test-ccy-info-flags.bash | 102 | 144 | 4 | I | 1 |  |  |
| files/home/.local/bin/rclone-cache-warm | 101 | 157 | 1 |  | 6 |  | ansible:play-rclone.yml,script:ftp-camera,script:rclone-cache-status,script:rclo |
| scripts/qa-ready-wait-rules.bash | 99 | 129 | 0 | J | 2 |  |  |
| files/home/.local/bin/rclone-rc-auth.bash | 96 | 236 | 4 |  | 7 | L | ansible:play-rclone.yml,script:ftp-camera,script:rclone-cache-status,script:rclo |
| files/usr/local/bin/compress | 96 | 127 | 2 |  | 2 |  | ansible:play-cli-tools.yml,script:clip-scan,script:open,script:uncompress |
| scripts/qa-ansible-pause.bash | 96 | 126 | 5 | I | 1 |  | ansible:clean.yml,ansible:flagged.yml |
| files/home/.local/bin/gshell-nested | 95 | 128 | 2 |  | 11 |  | ansible:play-gnome-shell-dev.yml |
| files/usr/local/sbin/fedora-desktop-self-update | 94 | 159 | 2 | I | 9 |  | ansible:fedora-desktop-self-update-verify.service.j2,ansible:fedora-desktop-self |
| scripts/qa-toolchain.bash | 92 | 142 | 3 |  | 4 |  | ansible:play-python.yml,ansible:qa.yml |
| files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend | 91 | 192 | 2 |  | 4 |  | ansible:play-suspend-and-lid-policy.yml |
| files/home/.local/bin/rdt | 90 | 120 | 6 |  | 1 |  | ansible:play-remote-desktop-toggle.yml |
| scripts/qa-helper-tests.bash | 90 | 169 | 0 | A | 5 |  | ansible:qa.yml,script:qa-helper-summary.bash |
| scripts/qa-ansible-syntax.bash | 88 | 180 | 0 | IJA | 4 |  |  |
| files/home/.local/bin/ssh-with-key | 87 | 142 | 2 | I | 5 |  | ansible:play-basic-configs.yml,script:scp-with-key,script:ssh-copy-id-with-key,s |
| extensions/scripts/gnome-shell-extract-js.bash | 85 | 124 | 6 | I | 4 |  |  |
| files/home/.local/bin/scp-with-key | 85 | 136 | 2 | I | 4 |  | ansible:play-basic-configs.yml,script:ssh-with-agent.bash |
| scripts/test-agent-mailbox-watch.bash | 85 | 105 | 2 |  | 1 |  |  |
| scripts/test-panel-sections.bash | 85 | 146 | 0 |  | 14 |  |  |
| .semgrep/ready-wait.bash | 84 | 114 | 9 |  | 2 |  |  |
| scripts/test-ccy-container-menus.bash | 83 | 116 | 6 |  | 1 |  |  |
| files/etc/profile.d/zz_lts-fedora-desktop.bash | 82 | 130 | 8 |  | 10 | L | script:history-search.bash,sourced:play-basic-configs.yml |
| scripts/test-terminal-modes.bash | 82 | 124 | 5 |  | 1 |  |  |
| scripts/test-ssh-suspend-guard.bash | 81 | 146 | 3 |  | 1 |  | script:ssh-suspend-guard |
| scripts/test-run-bash-ssh-agent-start.bash | 80 | 113 | 4 |  | 1 |  |  |
| scripts/test-run-bash-ssh-agent-teardown.bash | 78 | 136 | 2 |  | 1 |  |  |
| scripts/qa-docs.bash | 77 | 176 | 0 | J | 8 |  |  |
| files/var/local/ps1-prompt | 76 | 96 | 6 |  | 4 | L | ansible:play-basic-configs.yml,sourced:zz_lts-fedora-desktop.bash |
| scripts/setup.bash | 74 | 108 | 5 | I | 3 |  | ansible:github-required-scopes.yml,ansible:play-github-cli-multi.yml,script:fedo |
| files/home/.local/bin/ssh-copy-id-with-key | 73 | 122 | 2 | I | 1 |  | ansible:play-basic-configs.yml |
| scripts/test-qa-ansible-failfast.bash | 73 | 105 | 1 |  | 1 |  |  |
| scripts/test-qa-docs-exit-codes.bash | 72 | 153 | 2 | J | 2 |  |  |
| scripts/test-ccy-rootless-guard.bash | 71 | 115 | 1 |  | 1 |  | script:common-pure.bash |
| .semgrep/bash-conventions.bash | 69 | 206 | 0 | J | 4 |  |  |
| .claude/ccy/claude-supervise | 68 | 110 | 4 |  | 1 |  | script:common.bash,script:entrypoint.sh,script:restart-request.bash,script:sessi |
| files/home/.local/bin/bash-history-rank | 68 | 105 | 3 |  | 3 |  | ansible:play-basic-configs.yml,script:history-search.bash |
| scripts/qa-gh-scopes.bash | 66 | 97 | 0 | IJ | 1 |  |  |
| files/var/local/colours | 65 | 87 | 16 |  | 1 |  | ansible:play-basic-configs.yml,script:clip-scan,script:run.bash,script:tmux-sess |
| scripts/test-ccy-git-preflight.bash | 65 | 93 | 3 |  | 1 |  | script:entrypoint.sh |
| scripts/qa-plan-script-logging.bash | 64 | 136 | 1 | I | 3 |  |  |
| scripts/test-ccy-gitignore-safety.bash | 62 | 91 | 4 |  | 1 |  |  |
| scripts/test-ccy-host-hostname.bash | 62 | 111 | 2 |  | 1 |  |  |
| files/opt/claude-yolo/optional/agent-bus/agent-bus-claude | 58 | 96 | 1 |  | 3 |  | ansible:play-agent-bus-element.yml,ansible:play-claude-yolo.yml |
| scripts/agent-mailbox-watch.bash | 58 | 87 | 2 |  | 1 |  |  |
| files/home/bashrc-includes/history-search.bash | 57 | 99 | 2 | I | 5 |  | ansible:play-basic-configs.yml,script:bash-history-rank,script:zz_lts-fedora-des |
| scripts/qa-go.bash | 57 | 91 | 1 |  | 6 |  |  |
| scripts/test-ccy-project-env.bash | 56 | 82 | 2 |  | 1 |  | script:entrypoint.sh |
| scripts/desktop-symlinks | 55 | 82 | 0 |  | 5 |  |  |
| files/home/.local/bin/vmtest-bridge-watcher | 53 | 79 | 8 |  | 1 |  | ansible:play-vm-test-lab.yml,ansible:vmtest-bridge-heartbeat@.service,ansible:vm |
| scripts/test-ccy-gpu-device.bash | 53 | 86 | 1 |  | 1 |  |  |
| files/home/.local/bin/imgpaste | 51 | 80 | 2 | I | 3 |  | ansible:play-cli-tools.yml |
| scripts/qa-stt-limits.bash | 49 | 77 | 3 |  | 1 |  | script:wsi,script:wsi-stream |
| scripts/test-ccy-selinux-verdict.bash | 49 | 73 | 1 |  | 2 |  | script:common-pure.bash |
| files/opt/claude-yolo/optional/child-claude/bin/ccy-claude | 48 | 120 | 0 |  | 3 |  | ansible:play-claude-yolo.yml,script:entrypoint.sh |
| files/home/bashrc-includes/usb-audio-fix.bash | 47 | 69 | 1 |  | 2 |  | ansible:play-basic-configs.yml |
| scripts/test-ccy-flag-check.bash | 45 | 72 | 2 |  | 1 |  |  |
| scripts/test-vmtest-build-dir-cleanup.bash | 45 | 70 | 2 |  | 1 |  | script:vmtest |
| fedora-install/build-iso.bash | 43 | 68 | 1 |  | 3 |  |  |
| scripts/make-playbooks-executable.bash | 43 | 77 | 0 | I | 5 |  |  |
| files/usr/local/bin/debug-pipewire.bash | 39 | 41 | 0 | J | 3 |  |  |
| scripts/fedora-upgrade.bash | 38 | 68 | 0 |  | 3 |  |  |
| files/home/.local/bin/ssh-with-password | 36 | 77 | 0 |  | 5 |  | ansible:play-basic-configs.yml,script:scp-with-password,script:ssh-copy-id-with- |
| files/home/.local/bin/scp-with-password | 34 | 73 | 0 |  | 4 |  | ansible:play-basic-configs.yml,script:ssh-with-agent.bash |
| files/home/.local/bin/tmate-full | 34 | 56 | 0 |  | 3 |  | ansible:play-collaboration.yml,script:tmate-share |
| files/home/.local/bin/tmate-share | 34 | 55 | 0 |  | 3 |  | ansible:play-collaboration.yml,script:ready-wait.bash,script:tmate-full |
| files/usr/local/bin/ssh-suspend-guard | 34 | 77 | 3 |  | 2 |  | ansible:play-prevent-ssh-suspend.yml |
| files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash | 32 | 66 | 0 |  | 2 |  | ansible:lxc-docker-user-iptables-reconcile.service,ansible:play-lxc-install-conf |
| files/usr/local/bin/abrt-prune-stale.bash | 31 | 54 | 0 |  | 2 |  | ansible:abrt-prune-stale.service.j2,ansible:play-basic-configs.yml |
| files/home/.local/share/vmtest/guest-cleanup.bash | 30 | 61 | 0 |  | 2 |  | script:vmtest |
| files/home/.local/share/vmtest/vmtest-session-provision.bash | 30 | 55 | 0 |  | 2 |  | script:vmtest |
| .claude/skills/hooks-daemon/scripts/upgrade.sh | 28 | 61 | 0 |  | 4 |  | script:_locate-cli.sh,script:daemon-cli.sh,script:health-check.sh,script:init-ha |
| .claude/skills/docs-qa/scripts/_locate-cli.sh | 26 | 49 | 1 |  | 2 | L | sourced:find-comment-blocks.sh,sourced:sweep.sh |
| files/home/.local/bin/speedtest-isp | 24 | 44 | 0 | J | 1 |  | ansible:play-network-tools.yml |
| files/home/.local/bin/ssh-copy-id-with-password | 24 | 56 | 0 |  | 2 |  | ansible:play-basic-configs.yml |
| .claude/skills/hooks-daemon/scripts/_resolve-venv.sh | 20 | 49 | 0 |  | 3 |  | script:health-check.sh,script:init.sh |
| files/usr/local/bin/RapidRAW | 20 | 39 | 0 |  | 2 |  | ansible:play-photography.yml,ansible:play-rclone.yml,ansible:version-pins.yml |
| files/home/.local/bin/clean-paste | 19 | 66 | 0 |  | 1 |  | ansible:play-clean-paste.yml |
| files/home/.local/bin/wsi-setting | 17 | 45 | 0 |  | 1 |  | ansible:play-speech-to-text.yml,script:wsi,script:wsi-claude-process,script:wsi- |
| files/home/.local/bin/wsi-stop-grace | 17 | 38 | 0 |  | 3 |  | ansible:play-speech-to-text.yml,script:wsi,script:wsi-stream |
| files/var/local/claude-yolo/agent-browser-mode-required | 17 | 25 | 0 |  | 1 |  | ansible:play-claude-yolo.yml |
| .claude/hooks/status-line | 13 | 40 | 0 | J | 8 |  | script:init.sh |
| files/home/bashrc-includes/host-health-report.bash.j2 | 11 | 54 | 0 |  | 4 |  | ansible:play-host-health-login-report.yml |
| files/home/bashrc-includes/terminal-modes.bash | 11 | 35 | 1 |  | 1 |  | ansible:play-basic-configs.yml |
| .claude/hooks/stop | 10 | 34 | 0 |  | 6 |  | ansible:50-displaylink-dock-recovery.rules.j2,ansible:container-watch.service,an |
| .claude/hooks/subagent-stop | 10 | 34 | 0 |  | 6 |  | script:init.sh |
| .claude/hooks/config-change | 9 | 33 | 0 | J | 2 |  | ansible:play-rclone.yml,script:init.sh,script:optimise-invoke.sh |
| .claude/hooks/cwd-changed | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/elicitation | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/elicitation-result | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/file-changed | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/instructions-loaded | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/message-display | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/notification | 9 | 33 | 0 | J | 6 |  | ansible:host-health-report.bash.j2,ansible:host-health.service.j2,ansible:play-g |
| .claude/hooks/permission-denied | 9 | 33 | 0 | J | 2 |  | script:common.bash,script:init.sh |
| .claude/hooks/permission-request | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/post-compact | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/post-tool-batch | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/post-tool-use | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/post-tool-use-failure | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/pre-compact | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/pre-tool-use | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/session-end | 9 | 33 | 0 | J | 6 |  | script:init.sh,script:network-management.bash |
| .claude/hooks/session-start | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/setup | 9 | 33 | 0 | J | 2 |  | ansible:bash-conventions.yml,ansible:clean.yml,ansible:flagged.yml,ansible:githu |
| .claude/hooks/stop-failure | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/subagent-start | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/task-completed | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/task-created | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/teammate-idle | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/user-prompt-expansion | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/user-prompt-submit | 9 | 33 | 0 | J | 6 |  | script:init.sh |
| .claude/hooks/worktree-remove | 9 | 33 | 0 | J | 2 |  | script:init.sh |
| .claude/hooks/worktree-create | 8 | 35 | 0 |  | 4 |  | script:init.sh |
| .claude/skills/docs-qa/scripts/find-comment-blocks.sh | 8 | 31 | 0 |  | 2 |  |  |
| files/etc/profile.d/gh-multi-profile.sh | 7 | 24 | 1 |  | 1 |  | ansible:play-github-cli-multi.yml,script:gh-print-auth-url |
| files/usr/local/bin/gh-print-auth-url | 5 | 23 | 0 |  | 2 |  | ansible:play-github-cli-multi.yml,script:gh-account-setup.bash,script:gh-multi-p |
| scripts/vmtest-request.bash | 5 | 26 | 0 |  | 2 |  |  |
| .claude/skills/docs-qa/scripts/sweep.sh | 4 | 25 | 0 |  | 2 |  |  |
| files/home/.local/bin/container-watch | 2 | 22 | 0 |  | 1 |  | ansible:container-watch.service,ansible:play-claude-yolo.yml,ansible:play-contai |
| files/home/bashrc-includes/claude-devtools.bash | 2 | 6 | 0 |  | 1 |  | ansible:play-claude-devtools.yml |
| files/home/bashrc-includes/claude-yolo.bash | 2 | 10 | 0 |  | 3 |  | ansible:play-claude-yolo.yml |
| files/home/bashrc-includes/claude-yolo.bash.j2 | 2 | 11 | 0 |  | 1 |  | ansible:play-claude-yolo.yml |
| files/home/bashrc-includes/shutdown-with-update.bash | 2 | 7 | 0 |  | 5 |  | ansible:play-basic-configs.yml |
| files/usr/local/bin/github-ssh-443 | 2 | 26 | 0 |  | 1 |  | ansible:play-github-cli-multi.yml,script:zz-github-ssh-443.sh |
| files/etc/profile.d/zz-github-ssh-443.sh | 1 | 15 | 0 |  | 1 |  | ansible:play-github-cli-multi.yml |
