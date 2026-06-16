# GitHub Multi-Account Management

**Playbook**: `playbooks/imports/play-github-cli-multi.yml`
**Setup script**: `scripts/gh-account-setup.bash`
**Account definitions**: `environment/localhost/host_vars/localhost.yml` → `github_accounts`

This project supports multiple GitHub accounts on one machine, each with its own
SSH key, its own `gh` CLI authentication, an `~/.ssh/config` host alias, and a
full set of convenience shell functions (`git-<alias>`, `gh-<alias>`,
`clone-<alias>`, …). Commits, pushes, and `gh` commands are routed to the
correct identity automatically per repository.

## How It Works (read this first)

There are two moving parts and they have **distinct jobs**. Understanding the
split is the key to not getting confused:

| Component                                     | Responsibility                                                                                                                                                                                                        |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scripts/gh-account-setup.bash`               | **Authentication.** Logs each account into `gh` **with the required OAuth scopes**, generates the SSH key, uploads the public key to GitHub, and verifies SSH access.                                                 |
| `playbooks/imports/play-github-cli-multi.yml` | **Deployment.** Audits scopes, ensures SSH keys exist, writes the `~/.ssh/config` host-alias blocks, and regenerates the `git-<alias>` / `gh-<alias>` / `clone-<alias>` shell helper functions from the account list. |

The `github_accounts` dict in `localhost.yml` is the **single source of truth**.
Everything — SSH key names, SSH config host aliases, and every generated shell
function — derives from it.

### Do NOT authenticate with a bare `gh auth login`

⚠️ Running `gh auth login` by hand logs the account in **without the OAuth scopes
this project requires** (`admin:public_key`, `repo`, `workflow`, and others). The
playbook's scope audit will then **fail-fast** and send you back to the setup
script anyway.

**Always authenticate new accounts via `scripts/gh-account-setup.bash`** — it
bakes the correct scopes into the login. See [Required OAuth Scopes](#required-oauth-scopes).

## Initial Setup (first-time, all accounts)

The bootstrap (`run.bash`) prompts for accounts and calls the setup script for
you. To (re)run it manually for every account defined in `localhost.yml`:

```bash
./scripts/gh-account-setup.bash --setup-all
ansible-playbook playbooks/imports/play-github-cli-multi.yml
```

## Adding a New Account (the important workflow)

The playbook **does not** authenticate accounts — it only deploys keys, SSH
config, and shell helpers. Authentication is the setup script's job. Adding an
account is therefore a three-part flow (steps 1 and 2 can be collapsed — see the
note):

### 1. Add the account to the config

Edit `environment/localhost/host_vars/localhost.yml` and add the alias under
`github_accounts` (this is a regular YAML file — the account list is not
encrypted):

```yaml
# GitHub CLI accounts
github_accounts:
  personal: "your-personal-username"  # existing
  work: "your-work-username"          # existing
  oss: "your-oss-username"            # <-- new: alias is "oss", GitHub user is "your-oss-username"
```

The **alias** (left) is your local short name; the **value** (right) is the real
GitHub username.

### 2. Authenticate + generate the SSH key

```bash
./scripts/gh-account-setup.bash --add=oss:your-oss-username
```

This single command:

- logs `your-oss-username` into `gh` **with the required scopes** (opens a browser /
  prints a device-code URL),
- generates `~/.ssh/github_oss`,
- uploads the public key to GitHub (`gh ssh-key add`),
- verifies SSH access in isolation.

It is **idempotent with the config**: because you already added the alias in
step 1, it detects the existing entry and skips the config rewrite. (You may
skip step 1 entirely and let `--add` write the entry for you — either order
works.)

### 3. Deploy SSH config + shell helpers

```bash
ansible-playbook playbooks/imports/play-github-cli-multi.yml
```

This regenerates the per-account shell functions and writes the
`Host github.com-oss` block into `~/.ssh/config`.

### 4. Reload your shell

```bash
source ~/.bashrc
```

You now have `git-oss`, `gh-oss`, `clone-oss`,
`remote-oss`, `gh-token-oss`, etc., and SSH remotes of the form
`git@github.com-oss:owner/repo.git`.

> **Passphrase**: all `github_*` SSH keys share the single
> `github_ssh_passphrase` already stored in the vault. Adding an account needs
> no vault change.

## Required OAuth Scopes

Every account's `gh` token must carry these scopes. The canonical list lives in
a single source of truth — **`vars/github-required-scopes.yml`** — which both the
playbook (`github_required_scopes`) and the setup script (`REQUIRED_SCOPES`) read,
so the login/refresh and the audit can never request different scopes:

| Scope              | Why                                                   |
| ------------------ | ----------------------------------------------------- |
| `admin:public_key` | Programmatic SSH public-key upload (`gh ssh-key add`) |
| `gist`             | `gh gist` commands                                    |
| `project`          | GitHub Projects v2 (implies `read:project`)           |
| `read:org`         | Read org membership / list teams                      |
| `repo`             | Full repo access (clone, push, PRs, issues)           |
| `user:email`       | Read `user.email` for git config                      |
| `workflow`         | Push commits that modify `.github/workflows/*.yml`    |

The setup script grants these at login. The playbook audits them and
**fail-fasts** with a remediation command if any account is short.

## Available Commands

After setup (and `source ~/.bashrc`), these functions are available. Examples
use the `oss` alias.

### Account management

```bash
gh-list                    # List all configured accounts
gh-whoami                  # Show the currently active account
gh-status                  # Auth status for all accounts
gh-switch oss         # Switch the active gh account
github-test-ssh            # Test SSH connectivity for every account
git-accounts               # List configured git accounts + commands
git-which-account          # Show which account the current repo will use
```

### Per-account git (forces a specific identity)

```bash
git-oss push                  # Run git as oss (correct SSH key)
git-oss-branch-default        # Print the repo's default branch via oss
git-oss-checkout-default      # Check out the default branch via oss
```

Inside a repo whose remote uses a `github.com-<alias>` host, a plain `git` auto-
detects the account — no prefix needed.

### Per-account gh / clone / remote

```bash
gh-oss pr list                # Run a gh command as oss
clone-oss owner/repo          # Clone using the oss SSH key + remote
remote-oss owner/repo         # Point the current repo's remote at oss
gh-token-oss                  # Print a token for oss
gh-oss-token-phpstorm         # Generate a PhpStorm token with required scopes
git-init-with-account oss owner/repo   # git init with the oss remote
```

## Health Check

Verify every configured account is authenticated, scoped, keyed, and reachable
(read-only, makes no changes):

```bash
./scripts/gh-account-setup.bash --check
```

## Configuration Files

| Purpose         | Path                                                                     |
| --------------- | ------------------------------------------------------------------------ |
| Account list    | `environment/localhost/host_vars/localhost.yml` → `github_accounts`      |
| SSH private key | `~/.ssh/github_<alias>`                                                  |
| SSH public key  | `~/.ssh/github_<alias>.pub`                                              |
| SSH host alias  | `~/.ssh/config` (one `Host github.com-<alias>` block each)               |
| Shell functions | `~/.bashrc-includes/gh-aliases.inc.bash` (regenerated each playbook run) |
| Helper config   | `~/.config/git-account-helper/accounts.json`                             |

## Removing an Account

1. Remove the alias from `github_accounts` in `localhost.yml`.
2. Re-run the playbook — the shell functions regenerate without that account:
   ```bash
   ansible-playbook playbooks/imports/play-github-cli-multi.yml
   ```
3. Optionally remove the key material and SSH config block:
   ```bash
   rm ~/.ssh/github_<alias>*
   # then delete the "# ANSIBLE MANAGED: GitHub <alias>" block from ~/.ssh/config
   ```

## GitHub SSH over Port 443 (when port 22 is blocked)

Some networks (corporate, hotel, airport wifi) firewall outbound port 22, which
breaks `git@github.com` SSH. GitHub serves SSH on the HTTPS port at
`ssh.github.com:443` as an escape hatch. This repo exposes it as an **opt-in
toggle, off by default** — `ssh.github.com:443` presents the *same* host keys as
`github.com:22`, so it is a transparent endpoint swap, not a separate identity.

**Why it is off by default:** SSH-over-443 is raw SSH on port 443, **not** TLS. A
network that does HTTPS/TLS deep-packet-inspection on 443 will reject it — so
enabling it everywhere can *break* a connection that works fine on port 22. Turn
it on only when you actually need it.

### One signal, two layers, two lifetimes

There is a single runtime signal — the `GITHUB_SSH_443` environment variable —
and it drives **both** layers:

- **Host SSH** (desktop `git`, deploy keys, raw `ssh`) — controlled by a single
  first-wins override block at the **top** of `~/.ssh/config` plus a
  `[ssh.github.com]:443` `known_hosts` pin. Because OpenSSH uses the first value
  it sees per keyword, that one block reroutes plain `github.com`, the per-account
  `github.com-<alias>` aliases, **and** any deploy-key aliases you list — without
  editing each stanza (each keeps its own `IdentityFile`).
- **CCY container** — the entrypoint writes the container's own 443 config when
  `GITHUB_SSH_443=1` is set, so enabling 443 on the host propagates into ccy.

You can enable it **temporarily** (this bad network, no Ansible run) or
**always-on** (persisted, applied by Ansible).

> **ssh-agent: nothing to restart or flush.** The agent holds private-key
> identities, which are endpoint-agnostic — the same key authenticates identically
> on `github.com:22` and `ssh.github.com:443`. Routing and host verification come
> from `~/.ssh/config` / `~/.ssh/known_hosts`, re-read on every invocation. The
> only thing that can go stale is an SSH connection-multiplexing **control socket**
> pinned to `github.com:22`; this repo configures none, and the toggle closes any
> user-configured one (`ssh -O exit git@github.com`) anyway.

### Desktop host — temporary (no Ansible run)

Use the `github-ssh-443` CLI (deployed by `play-github-cli-multi.yml`):

```bash
github-ssh-443 status          # probe port 22 vs 443 and show current state
github-ssh-443 on              # route GitHub SSH over 443 (edits ~/.ssh/config + known_hosts)
github-ssh-443 auto            # enable only if port 22 is blocked AND 443 works
eval "$(github-ssh-443 env)"   # make THIS shell (and any ccy launched from it) use 443
github-ssh-443 off             # restore github.com:22 when you leave the network
```

`on`/`off` write the **same** managed blocks the playbook manages, so a later
Ansible run reconciles cleanly. If you have deploy-key Host aliases that redirect
to `github.com`, pass their globs so they are rerouted too:

```bash
github-ssh-443 on --aliases 'myorg_deploy_*,clientco_*'
```

> **Run `eval "$(github-ssh-443 env)"` too.** The per-account git wrappers
> (`git`, `git-<alias>`, `clone-<alias>`) force an isolated `ssh -F /dev/null`
> command that bypasses `~/.ssh/config`, so they follow the `GITHUB_SSH_443`
> **environment** signal, not the config block. Without the `eval`, plain `git`
> with a `git@github.com-<alias>:` remote still routes over 443 (via the config
> override), but the wrappers would stay on port 22. Always-on mode sets the env
> var for every shell via `/etc/profile.d`, so this only matters for the temporary
> CLI path.

### Desktop host — always-on (persisted)

Set the variable in `environment/localhost/host_vars/localhost.yml` and re-run the
playbook:

```yaml
github_ssh_over_443: true
# Optional: deploy-key Host alias globs that redirect to github.com, so the
# override reroutes them too.
github_443_extra_aliases:
  - myorg_deploy_*
```

```bash
ansible-playbook playbooks/imports/play-github-cli-multi.yml
ssh -T -p 443 git@ssh.github.com    # expect: "Hi <user>! You've successfully authenticated..."
```

Always-on also deploys `/etc/profile.d/zz-github-ssh-443.sh`, which exports
`GITHUB_SSH_443=1` for every login shell — so **every** `ccy` launch inherits 443
automatically. Set `github_ssh_over_443: false` and re-run to revert (the override
block, the `known_hosts` pin, and the profile export are all removed).

### CCY container

**Auto-fallback (no flag needed).** At launch CCY probes GitHub with your SSH key
over port 22. If that fails but `ssh.github.com:443` works, CCY detects the
port-22 block and offers to enable 443 for the session:

```text
⚠ GitHub SSH on port 22 failed, but ssh.github.com:443 works (authenticated as <user>).
  Port 22 is likely blocked on this network.
Enable GitHub SSH over 443 for this session? [Y/n]
```

Accept and the whole session — host-side probe, container git, host-key pinning —
routes over 443. Headless/non-interactive launches enable it automatically (it is
the only way to proceed). This is per-launch only; it does not persist.

Whenever a session ends up on 443 — by flag, inherited `GITHUB_SSH_443`, or
auto-detection — ccy prints a prominent **`🔒 GitHub SSH-over-443 MODE ACTIVE`**
banner at launch, naming which of the three triggered it, so the routing is never
a surprise.

**Force it explicitly** (skips the probe-and-prompt — e.g. you already know port 22
is blocked):

```bash
ccy --rebuild              # once, to pick up the entrypoint change
ccy --github-443           # launch with GitHub SSH routed over 443
export GITHUB_SSH_443=1    # equivalent host-level enable; ccy honours it
ccy                        #   → runs in 443 mode without the flag
```

Precedence inside ccy: `--github-443` flag → inherited `GITHUB_SSH_443=1` →
otherwise the auto-fallback probe above. This is why the always-on host export
(or `eval "$(github-ssh-443 env)"`) makes ccy follow the host toggle with no flag.

## Troubleshooting

| Symptom                                              | Cause / Fix                                                                                                                                                           |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Playbook fails: *"missing required OAuth scopes"*    | Account was logged in without the right scopes. Re-run `./scripts/gh-account-setup.bash --add=<alias>:<user>` (or `--setup-all`).                                     |
| Playbook fails: *"vault passphrase does not unlock"* | The existing `~/.ssh/github_<alias>` has a different passphrase. Delete it and re-run: `rm ~/.ssh/github_<alias>*`.                                                   |
| SSH verify fails despite key installed on GitHub     | Almost always passphrase-related. Diagnose: `ssh-keygen -y -P "$(cat /tmp/.github_ssh_pp)" -f ~/.ssh/github_<alias>`.                                                 |
| Commits attributed to the wrong account              | Run `git-which-account` in the repo; use `git-<alias>` to force the right identity, or fix the remote to `git@github.com-<alias>:owner/repo.git`.                     |
| `gh auth login` opened the wrong browser profile     | Open a **new** terminal (sources `/etc/profile.d/gh-multi-profile.sh`, which prints the device-code URL instead of guessing a browser), then re-run the setup script. |
| SSH hangs / times out on `git@github.com` (port 22)  | Network firewalls port 22. Enable SSH over 443 — see [GitHub SSH over Port 443](#github-ssh-over-port-443-when-port-22-is-blocked).                                   |
