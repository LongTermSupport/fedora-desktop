# GitHub SSH over Port 443 (for networks that block port 22)

Some networks — corporate/guest WiFi, hotels, conferences, locked-down client
sites — firewall outbound **TCP port 22**. That silently breaks Git-over-SSH to
GitHub: `git push`, `git pull`, `git clone git@github.com:…` all hang or fail with
`Connection refused` / `Connection timed out`.

GitHub publishes a second SSH endpoint, **`ssh.github.com:443`**, that serves the
**same host keys** and the **same authentication** as `github.com:22`. Routing your
GitHub SSH over it is a transparent endpoint swap — *not* a different account, key,
or identity. This repo gives you simple tools to switch to it when you need it and
switch back when you leave.

> **It's the PORT that matters, not the hostname.** `ssh.github.com` also listens
> on 22, so a port-22 firewall blocks it too. The only endpoint that survives is
> `ssh.github.com` on **port 443**.

---

## Am I affected? (10-second check)

```bash
timeout 8 bash -c 'echo > /dev/tcp/github.com/22'      && echo "22 open"  || echo "22 BLOCKED"
timeout 8 bash -c 'echo > /dev/tcp/ssh.github.com/443' && echo "443 open" || echo "443 BLOCKED"
```

If you see **`22 BLOCKED`** + **`443 open`**, this guide is for you.

---

## TL;DR — just fix it now

On the desktop host:

```bash
github-ssh-443 auto            # turns 443 on only if port 22 is blocked and 443 works
eval "$(github-ssh-443 env)"   # make this shell (and any ccy you launch) follow it
```

When you leave the restricted network:

```bash
github-ssh-443 off
```

That's it. Everything below is detail and the always-on option.

> `github-ssh-443` is installed by the GitHub playbook. If the command isn't found,
> run `ansible-playbook playbooks/imports/play-github-cli-multi.yml` once (or the
> all-in-one `./CLAUDE/Plan/00054-github-ssh-443-host-level/update.bash`).

---

## The `github-ssh-443` command

| Command                 | What it does                                                            |
| ----------------------- | ----------------------------------------------------------------------- |
| `github-ssh-443 status` | Probe port 22 vs 443 and show whether 443 mode is currently on          |
| `github-ssh-443 auto`   | Enable 443 **only if** port 22 is blocked and 443 works (the safe pick) |
| `github-ssh-443 on`     | Force 443 on — edits `~/.ssh/config` + pins the 443 host key            |
| `github-ssh-443 off`    | Restore normal `github.com:22`                                          |
| `github-ssh-443 env`    | Print `export`/`unset GITHUB_SSH_443` for `eval` (see below)            |

### Why you also run `eval "$(github-ssh-443 env)"`

Enabling 443 edits your `~/.ssh/config`, which is enough for **plain `git`** and
most tools. But this repo's **per-account git helpers** (`git`, `git-<alias>`,
`clone-<alias>`) deliberately use an isolated SSH command that bypasses
`~/.ssh/config` to guarantee the right key per account — so they instead follow the
`GITHUB_SSH_443` **environment variable**. Running `eval "$(github-ssh-443 env)"`
sets that variable in your current shell, so the helpers (and any `ccy` you launch
from that shell) route over 443 too.

You only need the `eval` for the temporary CLI path. The **always-on** option below
sets the variable for every shell automatically.

### Deploy keys / extra SSH host aliases

If you use per-repo deploy-key aliases that point at `github.com` (e.g.
`Host myorg_deploy_somerepo` → `HostName github.com`), list their patterns so they
get rerouted too:

```bash
github-ssh-443 on --aliases 'myorg_deploy_*,clientco_*'
```

---

## Three ways to turn it on

### 1. Temporary — this network only (no Ansible)

The `github-ssh-443 auto`/`on` + `eval` shown above. Reversible with
`github-ssh-443 off`. Use this when you're occasionally on a bad network.

### 2. Always-on — persisted (Ansible)

If you're *always* behind a port-22 firewall, make it the default. Set the variable
in `environment/localhost/host_vars/localhost.yml`:

```yaml
github_ssh_over_443: true

# Optional: deploy-key Host alias globs that point at github.com, rerouted too.
github_443_extra_aliases:
  - myorg_deploy_*
```

Then re-run the playbook:

```bash
ansible-playbook playbooks/imports/play-github-cli-multi.yml
ssh -T -p 443 git@ssh.github.com    # expect: "Hi <you>! You've successfully authenticated..."
```

Always-on also installs `/etc/profile.d/zz-github-ssh-443.sh`, which exports
`GITHUB_SSH_443=1` for **every** login shell — so the git helpers *and* every `ccy`
launch use 443 automatically, no `eval` needed. To turn it back off, set
`github_ssh_over_443: false` and re-run the playbook (the config override, the
host-key pin, and the profile export are all removed).

### 3. Inside CCY containers (`ccy`)

`ccy` handles 443 on its own:

- **Automatic.** At launch it probes GitHub over port 22; if that's blocked but
  `ssh.github.com:443` works, it offers to enable 443 for the session (and enables
  it automatically on headless/non-interactive launches).
- **Inherited.** If `GITHUB_SSH_443=1` is set in your shell (from always-on, or from
  `eval "$(github-ssh-443 env)"`), `ccy` picks it up with no flag.
- **Forced.** `ccy --github-443` always routes over 443.

Whenever a session ends up on 443, `ccy` prints a clear banner at launch so it's
never a surprise:

```text
════════════════════════════════════════════════════════════════════════════════
  🔒 GitHub SSH-over-443 MODE ACTIVE
════════════════════════════════════════════════════════════════════════════════
  GitHub SSH is routed over ssh.github.com:443 (port-22 firewall bypass).
  Trigger: auto-detected (port 22 blocked, 443 reachable)
════════════════════════════════════════════════════════════════════════════════
```

Precedence: `--github-443` flag → inherited `GITHUB_SSH_443=1` → auto-fallback probe.

---

## How it works (the short version)

There is one runtime signal — the `GITHUB_SSH_443` environment variable — and it
drives two layers:

- **Host SSH** (`git`, deploy keys, raw `ssh`): a single *first-wins* override block
  at the **top** of `~/.ssh/config`, plus a `[ssh.github.com]:443` entry in
  `~/.ssh/known_hosts`. Because OpenSSH uses the first value it sees for each
  setting, that one block reroutes `github.com`, the per-account
  `github.com-<alias>` aliases, and any deploy-key aliases you listed — while each
  per-key stanza keeps its own identity file.
- **CCY containers**: `ccy` writes its own equivalent config inside the container
  when `GITHUB_SSH_443=1`.

### Do I need to restart my ssh-agent? No.

The ssh-agent holds your **private keys**, which are endpoint-agnostic — the same
key authenticates identically on `github.com:22` and `ssh.github.com:443`. Routing
comes from `~/.ssh/config` and host verification from `~/.ssh/known_hosts`, both
re-read on every connection. Flipping 443 on or off needs **no agent restart or
flush**. (The only thing that could go stale is an SSH connection-multiplexing
*control socket* pinned to port 22; this repo configures none, and the toggle closes
any you may have set up anyway.)

---

## Reverting

- **Temporary:** `github-ssh-443 off`
- **Always-on:** set `github_ssh_over_443: false` and re-run
  `ansible-playbook playbooks/imports/play-github-cli-multi.yml`

Leaving 443 on is harmless on a normal network (it works everywhere), but turning it
off is good hygiene and the right move if you hit the DPI caveat below.

---

## Troubleshooting

| Symptom                                                     | Cause / Fix                                                                                                                                  |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `git push` hangs / `Connection refused` on `git@github.com` | Port 22 is firewalled. Run `github-ssh-443 auto` then `eval "$(github-ssh-443 env)"`.                                                        |
| `github-ssh-443: command not found`                         | The CLI isn't deployed yet. Run `ansible-playbook playbooks/imports/play-github-cli-multi.yml` (or `update.bash`), then open a new terminal. |
| 443 is on, plain `git` works, but `git-<alias>` still fails | The per-account helpers follow the env var — run `eval "$(github-ssh-443 env)"` (or use always-on mode).                                     |
| `ccy` doesn't route over 443                                | Set `export GITHUB_SSH_443=1` before launch, or use `ccy --github-443`, or rely on its auto-probe.                                           |
| Both 22 **and** 443 are blocked at the TCP level            | You may be on a DPI / TLS-only proxy that rejects raw SSH-over-443. Fall back to **HTTPS remotes** for read/pull, which *are* TLS.           |
| Need to `git pull` the repo itself but SSH is blocked       | Pull once over HTTPS: `git -c remote.origin.url=https://github.com/<owner>/<repo>.git pull origin <branch>`.                                 |

### The DPI caveat (when 443 also fails)

SSH-over-443 is *raw SSH on port 443* — it is **not** TLS. A network that does deep
packet inspection or only allows real TLS/HTTPS on 443 can still reject it. If your
10-second check shows `443 open` (TCP) but authentication still fails, you're likely
behind such a proxy. Use **HTTPS Git remotes** (which are genuine TLS) for read
paths, or an HTTPS-CONNECT proxy for SSH. This is also why 443 mode is **off by
default** — turning it on everywhere could break a connection that works fine on 22.

---

## What gets changed on disk

| Path                                   | When                                | Purpose                                              |
| -------------------------------------- | ----------------------------------- | ---------------------------------------------------- |
| `~/.ssh/config` (managed block at top) | `github-ssh-443 on` / always-on var | First-wins override routing GitHub SSH to 443        |
| `~/.ssh/known_hosts`                   | same                                | Pins the `[ssh.github.com]:443` host key (no prompt) |
| `/etc/profile.d/zz-github-ssh-443.sh`  | always-on var only                  | Exports `GITHUB_SSH_443=1` for every shell           |
| `/usr/local/bin/github-ssh-443`        | `play-github-cli-multi.yml`         | The toggle CLI                                       |

All managed blocks are clearly marked and removed cleanly when you turn 443 off.

---

## See also

- [GitHub Multi-Account Management](github-multi-account.md) — per-account SSH keys,
  `gh` auth, and the `git-<alias>` helpers this integrates with.
- GitHub's own docs:
  [Using SSH over the HTTPS port](https://docs.github.com/en/authentication/troubleshooting-ssh/using-ssh-over-the-https-port).
