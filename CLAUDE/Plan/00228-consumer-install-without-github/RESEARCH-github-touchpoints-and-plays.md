# Plan 00228 research: where GitHub is required, and which plays a consumer needs

Read from the checkout at the time the plan was filed. Line numbers are approximate
pointers into `run.bash` and drift as it changes; the function and variable names are the
stable anchors.

## 1. How the repository reaches a machine today

| Entry point                                                       | Fetch                                                                                   | GitHub account needed    |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------ |
| README / `docs/installation.md` one-liner                         | `curl` of `run.bash` from `raw.githubusercontent.com`, anonymous                        | no, for the fetch        |
| `fedora-install/ks.cfg` first-login autostart                     | `git clone` over HTTPS (`SETUP_REPO=https://github.com/...`), then `./run.bash`         | no, for the clone        |
| `run.bash` interactive, fresh box                                 | `gh_scopes_repo` may clone over HTTPS for the scope list; the main clone is **SSH**     | **yes**                  |
| `run.bash` interactive, existing checkout                         | origin is **rewritten to SSH** (`fedora_desktop_ssh_url`) on every run, then `git pull` | **yes**, on every re-run |
| `run.bash` headless, `RUN_BASH_GITHUB_ACCOUNTS=none` (Plan 00082) | HTTPS clone; an SSH origin is rewritten **to HTTPS**                                    | no                       |

So the anonymous HTTPS path already exists, but **only behind headless mode**. An
attended run has no way to say "no GitHub".

## 2. Where the attended run.bash requires GitHub

In order of execution in the `## Process` section:

01. **Login SSH key** — `ssh-keygen` of a passphrase-protected `~/.ssh/id`, prompted. Skipped
    only for headless `none`.
02. **GitHub CLI** — adds the `gh-cli.repo` and installs `gh` (the core play
    `play-git-configure-and-tools.yml` installs it again regardless).
03. **GitHub authentication** — `GH_HOST` written to `~/.bashrc`, scope audit through
    `helpers/github_scopes`, browser login (`choose_primary_gh_account`,
    `gh_request_missing_scopes`).
04. **SSH key upload** — `gh ssh-key add` of `~/.ssh/id.pub`.
05. **Known hosts** — `api.github.com/meta` fetched into `~/.ssh/known_hosts`.
06. **Clone/pull over SSH** — `git@github.com:LongTermSupport/fedora-desktop.git`.
07. **Personal configuration** — `config_repo="${primary_gh_username}/fedora-desktop-config"`:
    the private config repo is probed, pulled, merged or pushed (`config_repo_exists`,
    `config_repo_read`, `push_config_to_repo`). "Configure fresh" still calls
    `prompt_github_accounts_yaml`, which loops until at least one GitHub username is given.
08. **Vault** — `vault-pass.secret` is prompted or auto-generated; this part does not need
    GitHub, but the `!vault` values it protects mostly exist because of GitHub (next item).
09. **`github_ssh_passphrase`** — prompted and vault-encrypted into `localhost.yml`.
10. **`scripts/gh-account-setup.bash --setup-all`** — per-account auth, key generation and
    upload; runs whenever `localhost.yml` mentions `github_accounts`.
11. **Restore projects** — `fedora-install/pull-projects.bash` reads
    `projects.manifest` from the private config repo.
12. **Failure handling** — `create_github_issue` offers to file an issue with `gh`.

Outside `run.bash`:

- `fedora-install/push.bash` and `fedora-install/pull-projects.bash` are wholly about the
  private `<user>/fedora-desktop-config` repo. A consumer never runs them.
- `play-github-cli-multi.yml` already skips itself when `github_accounts` is empty
  (`github_accounts_configured`), so `github_accounts: {}` is enough for it.
- `play-git-configure-and-tools.yml` needs `user_email` and `user_name` for the git identity,
  imports `play-golang.yml`, and installs `gh`.
- `play-claude-yolo.yml`, `play-agent-bus.yml`: no GitHub login, but developer tooling.

What else is developer-shaped in the bootstrap itself: `run.bash` installs `golang` with
the base packages (for `gh-account-setup`), and the email prompt is mandatory.

## 3. Constraints found

- **`run.bash` is on the bash-limits ratchet** (`scripts/bash-limits.allowlist`): it may
  shrink, never grow. A consumer path has to be bought by generalising the existing
  headless `none` branches (one predicate instead of `HEADLESS == true && HL_GITHUB_ACCOUNTS == none`
  repeated at each site), or the new logic goes into Go.
- **`provisioning_profile` (desktop|server) is already taken** as the auto-detected
  axis (`environment/localhost/group_vars/desktop.yml`) and is asserted to be one of those
  two values in many plays. "Consumer" is an orthogonal axis (who uses the box), not a
  third value of that one.
- **The desktop profile grants `NOPASSWD: ALL`** (`play-basic-configs.yml`, "Passwordless
  Sudo"). That is a developer convenience and wrong for a non-technical user's machine.
- **The play scope guard pattern** (`scope: general|gnome|server` plus an `end_play` guard)
  is the existing mechanism a play uses to skip itself; an audience guard can follow the
  same shape.
- **The VM lab already provisions with `RUN_BASH_GITHUB_ACCOUNTS=none`** in every scenario
  but `server-github-token`, and has a `desktop-fresh-install` scenario on the `desktop`
  base (`vars/vm-test-scenarios.yml`).

## 4. Classification of the plays

**How judged:** by reading each play's name, its task names and its package lists, and
asking two questions. Does a person who browses, mails, plays media, prints and joins video
calls get any value from it? Does it assume someone who writes code, runs containers or uses
GitHub? "Shared" means it shapes the machine itself (updates, power, network, desktop
behaviour) and every user needs it. Where a play mixes both, it is marked shared with the
developer part named, to be gated inside the play.

### Main playbook (`playbooks/playbook-main.yml`)

| Play                               | Class          | Why                                                                              |
| ---------------------------------- | -------------- | -------------------------------------------------------------------------------- |
| `play-AA-preflight-sanity.yml`     | shared         | Fedora and Ansible checks                                                        |
| `play-AB-dnf-upgrade.yml`          | shared         | system upgrade, half-installed kernel repair                                     |
| `play-basic-configs.yml`           | shared (mixed) | base packages, bash tweaks; **NOPASSWD sudo block must be absent for consumers** |
| `play-prevent-ssh-suspend.yml`     | developer-only | keeps a box awake for SSH sessions                                               |
| `play-suspend-and-lid-policy.yml`  | shared         | laptop suspend and lid policy                                                    |
| `play-network-wait-tuning.yml`     | shared         | boot speed                                                                       |
| `play-mask-intel-lpmd.yml`         | shared         | hardware fix                                                                     |
| `play-systemd-user-tweaks.yml`     | shared (mixed) | oomd tweak is shared; user linger exists for long-running agent services         |
| `play-nvm-install.yml`             | developer-only | Node.js toolchain                                                                |
| `play-git-configure-and-tools.yml` | developer-only | git identity (needs email), git prompt, `gh`, imports `play-golang.yml`          |
| `play-golang.yml` (imported)       | developer-only | Go toolchain                                                                     |
| `play-git-hooks-security.yml`      | developer-only | global git hooks for secret scanning                                             |
| `play-firefox.yml`                 | consumer       | browser                                                                          |
| `play-github-cli-multi.yml`        | developer-only | GitHub accounts and keys (already a no-op on `github_accounts: {}`)              |
| `play-fonts.yml`                   | consumer       | fonts                                                                            |
| `play-rpm-fusion.yml`              | consumer       | codecs and media                                                                 |
| `play-browsers.yml`                | consumer       | Chrome, Brave, Vivaldi (how many a consumer wants is a Decision)                 |
| `play-toolbox-install.yml`         | developer-only | JetBrains Toolbox                                                                |
| `play-lxc-install-config.yml`      | developer-only | LXC containers, firewall zone for them                                           |
| `play-podman.yml`                  | developer-only | container engine (ccy depends on it)                                             |
| `play-python.yml`                  | developer-only | pipx, pyenv deps, pinned QA tools, shellcheck, golangci-lint                     |
| `play-claude-yolo.yml`             | developer-only | Claude Code host `cc` and container `ccy`                                        |
| `play-comms.yml`                   | consumer?      | Slack Flatpak; a work tool, so a Decision                                        |
| `play-gnome-shell.yml`             | shared         | GNOME packages                                                                   |
| `play-gnome-shell-extensions.yml`  | shared (mixed) | extensions; any developer-only extension (e.g. agent or ccy panels) gated inside |
| `play-markless.yml`                | developer-only | terminal Markdown viewer                                                         |
| `play-terminal-emulators.yml`      | developer-only | Alacritty, Kitty, Ghostty, Foot                                                  |
| `play-tmux-sessions.yml`           | developer-only | tmux menu                                                                        |
| `play-vscode.yml`                  | developer-only | VS Code                                                                          |
| `play-vpn.yml`                     | shared         | WireGuard and OpenVPN for NetworkManager                                         |
| `play-agent-bus.yml`               | developer-only | agent team homeserver                                                            |
| `play-gsettings.yml`               | shared (mixed) | Caps Lock off, Text Editor windows are shared; the Ptyxis tweak is harmless      |
| `play-ZZ-repo-cleanup.yml`         | shared         | removes orphaned repos                                                           |

### Optional plays (`playbooks/imports/optional/`), first pass

Consumer-relevant candidates, to be confirmed by the owner (Decision 1):
`common/play-photography.yml`, `common/play-videography.yml`, `common/play-qobuz.yml`,
`common/play-hd-audio.yml`, `common/play-image-watermarking.yml`,
`common/play-remote-desktop-toggle.yml`, `common/play-lastpass.yml`,
`common/play-collaboration.yml`, `common/play-nordvpn-openvpn.yml`,
`common/play-cloudflare-warp.yml`, `common/play-rclone.yml`,
`common/play-speech-to-text.yml`, and every `hardware-specific/` play (DisplayLink, IPU6
webcam, MusicCast, NVIDIA; laptop thermal diagnostics is a diagnostic tool, so developer).

Developer-only: `play-advanced-kernel-management.yml`, `play-agent-bus-element.yml`,
`play-claude-devtools.yml`, `play-clean-paste.yml`, `play-cli-tools.yml`,
`play-cloudflare-dns.yml`, `play-container-watch.yml`, `play-ddev.yml`,
`play-distrobox.yml`, `play-docker.yml`, `play-fast-file-manager.yml`,
`play-fedora-desktop-panel.yml`, `play-ftp-camera.yml`, `play-gnome-shell-dev.yml`,
`play-host-health-login-report.yml`, `play-lightweight-ides.yml`, `play-lxcfreeze.yml`,
`play-md-viewer.yml`, `play-network-tools.yml`, `play-podfreeze.yml`, `play-rust-dev.yml`,
`play-self-update.yml` (server scope today), `play-unifi-controller.yml`,
`play-unifi-firmware.yml`, `play-vm-test-lab.yml`, everything under `experimental/` and
`archived/`.

This first pass was made from names and headers only for the optional set; Task 1.2 reads
each one before its `audience` is set.
