# Headless Server Install — Unattended IaC Runbook

A precise, copy-paste, step-by-step runbook for provisioning a **fresh Fedora Server
or Fedora Cloud** box with `run.bash` in fully unattended (headless) mode. Written to
be followed literally by a human **or an LLM agent** operating the box — every step
has exact commands and an expected result.

If you want the reference contract (the full variable table, how headless mode is
triggered, the security model), see [Headless / Unattended Provisioning](headless-provisioning.md)
or run `./run.bash --help-run-headless`. **This page is the how-to; that page is the
what.**

---

## Mental model (read this first)

The whole unattended install is four moves:

1. **Download** `run.bash` onto the box (and inspect it — never pipe it into a shell).
2. **Create the secret files** — three `0600` files holding a GitHub token, an SSH key
   passphrase, and an Ansible vault password. The secret *bytes* live only in these
   files.
3. **Write the vars file** (`run-bash.env`) — a shell file that `export`s the
   `RUN_BASH_*` configuration variables. It contains **non-secret** config plus the
   **paths** to the three secret files (never the secret bytes themselves).
4. **Run** — source the vars file and execute `run.bash` as the non-root target user.

`run.bash` then provisions the box end-to-end with zero prompts: it self-updates the
repo to the branch-latest source, writes the Ansible config, authenticates to GitHub
with the token, and runs the main playbook (server profile — no GNOME/desktop plays).

**Fail-fast, fail-loud:** any missing input or any failure aborts immediately with a
big red banner naming the step, the reason, and a debug pointer, and exits non-zero.
It never hangs on a prompt and never reports success on failure.

---

## Prerequisites (hard requirements)

| Requirement                   | Why                                                                                                                                                                      | How to check                                             |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------- |
| Fresh Fedora Server/Cloud     | The version must match the repo branch (e.g. F44 = Fedora 44).                                                                                                           | `cat /etc/fedora-release`                                |
| A **non-root** target user    | `run.bash` refuses to run as root (it uses `sudo` internally).                                                                                                           | `whoami` (must not be `root`)                            |
| **ALL-scoped sudo**           | Headless `sudo`/Ansible `become` cannot answer a prompt, so the credential must be supplied. Either `NOPASSWD:ALL` **or** password sudo + `RUN_BASH_SUDO_PASSWORD_FILE`. | `sudo -k -n true` (exit 0 = NOPASSWD); otherwise Step 0b |
| Network access                | GitHub, DNF repos, Ansible Galaxy.                                                                                                                                       | `curl -fsS https://github.com >/dev/null && echo ok`     |
| A GitHub account + scoped PAT | GitHub is mandatory in headless v1.                                                                                                                                      | see Step 2a                                              |

> **Cloud images** (Fedora Cloud, most cloud-init distros) already create a non-root
> user with `NOPASSWD` sudo — you are running as it, and Step 0b does not apply.

> **A COMMAND-scoped sudoers rule is not supported, by either route.** `sudo -k -n true`
> passes on a rule that only permits `/bin/true`, and the run then dies at the first
> `dnf`. Both credentials must be `ALL`-scoped. The probe cannot detect this — it is a
> known limitation, stated rather than implied.

---

## Step 0 — Become the target user and verify sudo

Run everything below as the **non-root** user that will own the machine.

```bash
whoami            # must NOT print 'root'
sudo -k -n true && echo "NOPASSWD sudo: OK" || echo "NOPASSWD sudo: MISSING"
```

If it prints `OK`, you are done — skip to Step 1. If it prints `MISSING`, pick **one**
of the two routes below.

### Step 0a — grant `NOPASSWD:ALL` (simplest)

```bash
echo "$(whoami) ALL=(ALL) NOPASSWD:ALL" | sudo tee "/etc/sudoers.d/90-$(whoami)-nopasswd"
sudo chmod 0440 "/etc/sudoers.d/90-$(whoami)-nopasswd"
sudo -k -n true && echo "NOPASSWD sudo: OK"
```

### Step 0b — keep password sudo, supply the password in a file

Prefer this when the user must **not** hold permanent passwordless root — for example a
CI box where a locked-down account reaches this user through a single `NOPASSWD` rule,
and a `NOPASSWD:ALL` here would complete the ladder to root.

```bash
sudo install -d -m 0700 -o "$(whoami)" -g "$(whoami)" /run/secrets   # tmpfs — RAM-backed
install -m 0600 /dev/null /run/secrets/sudo-pass
read -rs -p 'sudo password: ' p; echo; printf '%s' "$p" > /run/secrets/sudo-pass; unset p
```

Then add `RUN_BASH_SUDO_PASSWORD_FILE=/run/secrets/sudo-pass` to the invocation in
Step 3. `run.bash` proves the password authenticates during preflight — before any
provisioning action — so a wrong password fails in seconds, not mid-install.

**Be clear about what this buys.** *During* the run the two routes are equivalent:
anything running as this user can reach root either way. The gain is in the **steady
state** (no permanent passwordless root once the run ends) and in the **failure mode**
— a sudoers file that fails to be removed leaves passwordless root forever, whereas a
password file on tmpfs that fails to be shredded dies at the next boot and never
touched persistent storage.

---

## Step 1 — Download and inspect run.bash

Download to the box, pin to a known ref, and read it before running. **Do not**
`curl … | bash` — that runs unread remote code.

```bash
# Pick a ref: a branch (matches your Fedora version) or, for reproducibility, a commit SHA.
REF=F44                      # or a full commit SHA for a pinned, immutable install

curl -fsSL -o ~/run.bash \
  "https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/${REF}/run.bash"

chmod +x ~/run.bash
less ~/run.bash              # inspect: confirm it is the expected script
```

You can preview the full unattended contract straight from the downloaded file:

```bash
~/run.bash --help-run-headless
```

---

## Step 2 — Create the three secret files (`0600`)

The secret bytes live only in these files. Put them on `tmpfs` (`/run/secrets`,
RAM-backed, wiped on reboot) and lock them to `0600`, owned by the target user.

```bash
sudo install -d -m 0700 -o "$(whoami)" -g "$(whoami)" /run/secrets
```

### 2a — GitHub Personal Access Token → `/run/secrets/gh-token`

Create a **classic PAT** on github.com (Settings → Developer settings → Personal
access tokens → Tokens (classic)) with these scopes:

- every scope listed in `vars/github-required-scopes.yml` in the repo, **plus**
- `admin:public_key` (so `run.bash` can upload your SSH key).

The token must be **fully scoped up front** — headless cannot run the interactive
scope-refresh flow (it will fail loud if the token is under-scoped).

Write it to the file (paste the token at the prompt; it is not echoed):

```bash
read -rs -p 'Paste GitHub PAT: ' GH_PAT; echo
printf '%s' "$GH_PAT" > /run/secrets/gh-token
chmod 600 /run/secrets/gh-token
unset GH_PAT
```

### 2b — SSH key passphrase → `/run/secrets/ssh-pass`

`run.bash` generates a **passphrase-protected** login SSH key (`~/.ssh/id`) and loads
it via `ssh-agent`. Choose or generate a passphrase and store it:

```bash
# Generate a strong random passphrase (or substitute your own):
openssl rand -base64 24 > /run/secrets/ssh-pass
chmod 600 /run/secrets/ssh-pass
```

### 2c — Ansible vault password → `/run/secrets/vault-pass`

On a **fresh** box `run.bash` uses this to vault-encrypt `github_ssh_passphrase` into a
new `localhost.yml`. Choose a strong password (**save it in your password manager** —
you need it to decrypt this box's config later):

```bash
openssl rand -base64 24 > /run/secrets/vault-pass
chmod 600 /run/secrets/vault-pass
```

> If you are re-provisioning from an **existing** encrypted config (via
> `RUN_BASH_CONFIG_SOURCE`), this file must hold the **existing** vault password that
> encrypted that config — `run.bash` verifies it and aborts loud if it does not
> decrypt. It never auto-generates over encrypted values.

Confirm the three files exist and are locked down:

```bash
ls -l /run/secrets/gh-token /run/secrets/ssh-pass /run/secrets/vault-pass
# each must show: -rw------- (0600), owned by your user
```

---

## Step 3 — Write the vars file (`run-bash.env`)

This shell file `export`s the configuration. It holds **non-secret** values plus the
**paths** to the secret files from Step 2 — never the secret bytes. Adjust the values,
then save it as `~/run-bash.env`.

```bash
cat > ~/run-bash.env <<'ENV'
# run-bash.env — headless provisioning config for run.bash
# Secret BYTES live in the 0600 files below; this file only names their PATHS.

# ── Turn headless on ─────────────────────────────────────────────────────────
export RUN_BASH_HEADLESS=1

# ── Required identity + GitHub (v1) ──────────────────────────────────────────
export RUN_BASH_USER_EMAIL="you@example.com"          # your git email
export RUN_BASH_GITHUB_ACCOUNTS="your-gh-username"     # single GitHub username

# ── Required secret FILE POINTERS (0600 files from Step 2) ───────────────────
export RUN_BASH_GITHUB_TOKEN_FILE="/run/secrets/gh-token"
export RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE="/run/secrets/ssh-pass"
export RUN_BASH_VAULT_PASSWORD_FILE="/run/secrets/vault-pass"

# ── Sudo credential: ONLY if this user does NOT have NOPASSWD:ALL (Step 0b) ──
# export RUN_BASH_SUDO_PASSWORD_FILE="/run/secrets/sudo-pass"

# ── Optional (sensible defaults shown; delete any you don't need) ────────────
export RUN_BASH_HOSTNAME="my-server"                  # only applied if box is still 'fedora'
export RUN_BASH_PROVISIONING_PROFILE="server"         # or omit to auto-detect
export RUN_BASH_CONFIG_SOURCE="none"                  # or hosts/<name>.yml from your private config repo
export RUN_BASH_OPTIONAL_PLAYBOOKS="none"             # or "play-docker.yml play-podman.yml"
export RUN_BASH_RESTORE_PROJECTS=0                    # 1 = restore projects from config manifest
export RUN_BASH_REBOOT=0                              # 1 = reboot at the end
ENV

chmod 600 ~/run-bash.env
```

**Required** keys: `RUN_BASH_USER_EMAIL`, `RUN_BASH_GITHUB_ACCOUNTS`,
`RUN_BASH_GITHUB_TOKEN_FILE`, `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` (and
`RUN_BASH_VAULT_PASSWORD_FILE` for any config that uses a vault — which the default
fresh install does). Everything under "Optional" can be deleted to take the default.

> **Secrets, in-line:** the literal forms (`RUN_BASH_GITHUB_TOKEN`,
> `RUN_BASH_GITHUB_SSH_PASSPHRASE`, `RUN_BASH_VAULT_PASSWORD`) are supported but
> **refused on a detected cloud box** and warned against elsewhere — always prefer the
> `*_FILE` pointers above.

---

## Step 4 — Run it

Source the vars file (so the `RUN_BASH_*` exports enter the environment), then run:

```bash
set -a; source ~/run-bash.env; set +a
~/run.bash
```

(The `set -a`/`set +a` is belt-and-braces; the file already uses `export`, so a plain
`source ~/run-bash.env && ~/run.bash` also works.)

That is the entire install. `run.bash` now runs unattended to completion.

---

## Step 5 — What success and failure look like

**Success:** the run streams `✓` step lines and ends with a green
`HEADLESS PROVISIONING complete` / `ALL DONE!` banner and exit code `0`. If you set
`RUN_BASH_REBOOT=1`, the box reboots; otherwise it stays up for you to reboot.

**Failure:** the run stops at the first problem with an unmistakable banner and a
non-zero exit code:

```
╔════════════════════════════════════════════════════════════════╗
║  HEADLESS PROVISIONING FAILED — run.bash vX.Y.Z                 ║
╚════════════════════════════════════════════════════════════════╝
  STEP : <what was being done>
  WHY  : <the concrete reason>
  DEBUG: <exactly what to check / how to fix>
```

Common first-run failures and their fix:

| Banner STEP                                              | Cause                                                                                                                  | Fix                                                                                |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `Headless run cannot proceed` (preflight)                | A required var/secret is missing or unsafe.                                                                            | Read the WHY — it names the exact `RUN_BASH_*` to set.                             |
| `neither passwordless sudo nor a supplied sudo password` | Target user has no usable sudo credential.                                                                             | Step 0a (grant `NOPASSWD:ALL`) **or** Step 0b (set `RUN_BASH_SUDO_PASSWORD_FILE`). |
| `RUN_BASH_SUDO_PASSWORD did not authenticate`            | The supplied password is wrong for this user, or the user is not in sudoers at all — the banner quotes what sudo said. | Fix the file from Step 0b; check `sudo -v` works by hand.                          |
| GitHub token auth                                        | PAT rejected or under-scoped.                                                                                          | Recreate the PAT with all required scopes (Step 2a).                               |
| load login SSH key into ssh-agent                        | Wrong SSH passphrase file.                                                                                             | Ensure `/run/secrets/ssh-pass` matches the key's passphrase.                       |
| vault reconcile                                          | Vault password does not decrypt existing config.                                                                       | Provide the correct vault password (Step 2c).                                      |
| main playbook                                            | An Ansible task failed.                                                                                                | Scroll up to the failing task's output; fix the cause; re-run.                     |

Because the whole run is fail-loud, an LLM agent driving the box can capture the banner
and act on the `WHY`/`DEBUG` lines directly.

---

## Idempotency & re-running

`run.bash` is safe to re-run. On a second run it keeps an already-configured
`localhost.yml`, an existing SSH key, and an existing verified vault password, and
Ansible tasks are idempotent. If a run fails partway, fix the cause named in the banner
and simply run Step 4 again.

---

## Full cloud-init example (end-to-end, hands-off)

This provisions the box on first boot with **no** SSH session at all. Fetch the secrets
**out-of-band** in `runcmd` (never in `write_files` — that content is served by the
metadata service forever). Replace `<user>` with the cloud image's default user and the
`get-secret` commands with your secret store.

```yaml
#cloud-config
runcmd:
  # 1. Fetch secrets to 0600 tmpfs files (example uses AWS Secrets Manager).
  - [ sh, -c, 'install -d -m 0700 -o <user> -g <user> /run/secrets' ]
  - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id gh-token   --query SecretString --output text > /run/secrets/gh-token   && chmod 600 /run/secrets/gh-token   && chown <user>:<user> /run/secrets/gh-token' ]
  - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id ssh-pass   --query SecretString --output text > /run/secrets/ssh-pass   && chmod 600 /run/secrets/ssh-pass   && chown <user>:<user> /run/secrets/ssh-pass' ]
  - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id vault-pass --query SecretString --output text > /run/secrets/vault-pass && chmod 600 /run/secrets/vault-pass && chown <user>:<user> /run/secrets/vault-pass' ]

  # 2. Download run.bash (pin to a SHA for reproducibility).
  - [ sh, -c, 'curl -fsSL -o /home/<user>/run.bash https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/F44/run.bash && chmod +x /home/<user>/run.bash && chown <user>:<user> /home/<user>/run.bash' ]

  # 3. Run headless as the non-root user, config passed inline as env.
  - [ sh, -c, 'sudo -u <user> -i env
        RUN_BASH_HEADLESS=1
        RUN_BASH_USER_EMAIL=you@example.com
        RUN_BASH_GITHUB_ACCOUNTS=your-gh-username
        RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token
        RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/run/secrets/ssh-pass
        RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass
        RUN_BASH_REBOOT=1
        /home/<user>/run.bash' ]
```

---

## Appendix — one-shot install script

For an SSH-in workflow, this wraps Steps 1–4 into a single script. Fill in the three
`read`/values and run it as the non-root user.

```bash
#!/usr/bin/env bash
set -euo pipefail

REF=F44
GH_USERNAME="your-gh-username"
GIT_EMAIL="you@example.com"

# 1. run.bash
curl -fsSL -o ~/run.bash "https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/${REF}/run.bash"
chmod +x ~/run.bash

# 2. secrets (0600 on tmpfs)
sudo install -d -m 0700 -o "$(whoami)" -g "$(whoami)" /run/secrets
read -rs -p 'GitHub PAT: '           _gh;   echo; printf '%s' "$_gh"  > /run/secrets/gh-token;   unset _gh
read -rs -p 'SSH key passphrase: '   _ssh;  echo; printf '%s' "$_ssh" > /run/secrets/ssh-pass;   unset _ssh
read -rs -p 'Ansible vault password: ' _v;   echo; printf '%s' "$_v"   > /run/secrets/vault-pass; unset _v
chmod 600 /run/secrets/gh-token /run/secrets/ssh-pass /run/secrets/vault-pass

# 2b. ONLY if this user lacks NOPASSWD:ALL — uncomment both lines here and the export below
# read -rs -p 'sudo password: '        _sp;   echo; printf '%s' "$_sp"  > /run/secrets/sudo-pass
# chmod 600 /run/secrets/sudo-pass; unset _sp

# 3 + 4. config + run
export RUN_BASH_HEADLESS=1
export RUN_BASH_USER_EMAIL="$GIT_EMAIL"
export RUN_BASH_GITHUB_ACCOUNTS="$GH_USERNAME"
export RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token
export RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/run/secrets/ssh-pass
export RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass
# export RUN_BASH_SUDO_PASSWORD_FILE=/run/secrets/sudo-pass   # only with step 2b above
export RUN_BASH_PROVISIONING_PROFILE=server
exec ~/run.bash
```

---

## See also

- [Headless / Unattended Provisioning](headless-provisioning.md) — the reference contract (full variable table, trigger rules, security model)
- `./run.bash --help-run-headless` — the authoritative, always-current contract built into the script
- [Installation Guide](installation.md) — the interactive desktop install
- [GitHub Multi-Account Setup](github-multi-account.md) — the account model behind `github_accounts`
