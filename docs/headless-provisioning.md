# Headless / Unattended Provisioning (Server & Cloud)

`run.bash` can provision a **Fedora Server** or **Fedora Cloud** box end-to-end with
**zero interactive prompts**, driven entirely by `RUN_BASH_*` environment variables.
This is the path for IaC / cloud-init / CI provisioning where no human is at the
keyboard.

It is the same `run.bash` used interactively — headless mode simply supplies every
answer up front from the environment, runs on the box it provisions
(`connection: local`), self-updates the repo to the branch-latest source, and lets the
Ansible layer auto-detect the **server** profile (skipping all GNOME/desktop plays,
per Plan 00061). Fedora Cloud resolves to the server profile too — no separate scope.

> **The authoritative, always-current contract is built into the script:**
>
> ```bash
> ./run.bash --help-run-headless
> ```
>
> This document is the narrative companion; if the two ever disagree, the `--help`
> output wins.

## When headless mode turns on

Headless is ON when **any** of these hold:

- `--headless` flag is passed, or `RUN_BASH_HEADLESS=1` is set; or
- stdin is **not** a TTY **and** at least one `RUN_BASH_*` config var is set.

Force it **off** with `--interactive` (useful for piped-stdin smoke tests).

## Preconditions (fail fast — a headless run never hangs)

- **Run as the non-root target user.** cloud-init `runcmd` is root; drop to the user
  (`sudo -u <user> -i …`).
- **Passwordless (`NOPASSWD:ALL`) sudo** for that user. The default cloud image user
  has it; a password-sudo Server does not — configure `NOPASSWD` or run interactively.
- **GitHub is mandatory in v1.** You must provide a single GitHub account **and** a
  scoped token (see below). The GitHub-empty (`none`) path is a planned follow-up and
  currently fails fast.

Any missing or unsafe input aborts immediately with a **big, specific error** naming
the exact fix — a headless run never blocks waiting on a prompt that can't be answered.

## The environment contract

### Non-secret configuration (plain `RUN_BASH_*`)

| Variable                        | Meaning                                                               | Required            |
| ------------------------------- | --------------------------------------------------------------------- | ------------------- |
| `RUN_BASH_HEADLESS=1`           | Force headless mode.                                                  | —                   |
| `RUN_BASH_USER_EMAIL`           | Git email.                                                            | **Yes**             |
| `RUN_BASH_GITHUB_ACCOUNTS`      | Single GitHub username (v1). `alias:user` also accepted.              | **Yes**             |
| `RUN_BASH_USER_LOGIN`           | System login.                                                         | No (current user)   |
| `RUN_BASH_USER_NAME`            | Full name.                                                            | No (= login)        |
| `RUN_BASH_HOSTNAME`             | Hostname to set when the box is still named `fedora`.                 | No (leaves default) |
| `RUN_BASH_CONFIG_SOURCE`        | `hosts/<name>.yml` to import from the private config repo, or `none`. | No (`none` = fresh) |
| `RUN_BASH_PROVISIONING_PROFILE` | Force `desktop`/`server`.                                             | No (auto-detect)    |
| `RUN_BASH_OPTIONAL_PLAYBOOKS`   | Space/comma list of optional plays (`play-foo.yml`/`foo`), or `none`. | No (`none`)         |
| `RUN_BASH_RESTORE_PROJECTS`     | `1` to restore projects from the config manifest.                     | No (off)            |
| `RUN_BASH_REBOOT`               | `1` to reboot at the end.                                             | No (off)            |

### Secrets — prefer `0600` file pointers

Provide each secret as a **path to a `0600` file** (recommended) — the secret bytes
never enter the environment, process listings, or cloud-init user-data:

| Variable                              | Secret                 | Required                      |
| ------------------------------------- | ---------------------- | ----------------------------- |
| `RUN_BASH_GITHUB_TOKEN_FILE`          | Scoped GitHub PAT      | **Yes** (v1)                  |
| `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` | SSH key passphrase     | **Yes** (v1)                  |
| `RUN_BASH_VAULT_PASSWORD_FILE`        | Ansible vault password | When your config uses a vault |

Literal equivalents (`RUN_BASH_GITHUB_TOKEN`, `RUN_BASH_GITHUB_SSH_PASSPHRASE`,
`RUN_BASH_VAULT_PASSWORD`) are accepted but:

- **Refused on a detected cloud box** — cloud-init persists user-data in the metadata
  service, world-readable indefinitely. Use the `*_FILE` form there.
- **Warned loudly** otherwise, and setting **both** a literal and its `*_FILE` is an
  error.

**GitHub token scope:** the full `vars/github-required-scopes.yml` set **plus**
`admin:public_key`. The login SSH key stays passphrase-protected (it is loaded
non-interactively via `ssh-agent` + a transient `SSH_ASKPASS` helper), which is why
the passphrase file is required.

## Canonical invocation

Run as the **non-root** target user, branch-latest repo, full setup:

```bash
RUN_BASH_HEADLESS=1 \
RUN_BASH_USER_EMAIL=name@example.com \
RUN_BASH_GITHUB_ACCOUNTS=<gh-username> \
RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token \
RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE=/run/secrets/ssh-pass \
RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass \
  ./run.bash
```

`/run/secrets` is `tmpfs` (RAM-backed, wiped on reboot) — a good home for the `0600`
secret files.

## Cloud-init (Fedora Cloud) — fetch secrets out-of-band

`write_files` embeds its content **inside** user-data, which the metadata service
serves forever — so **never** put secret bytes there. Fetch them out-of-band inside
`runcmd`, immediately above the `run.bash` line:

```yaml
runcmd:
  - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id vault
        --query SecretString --output text > /run/secrets/vault-pass' ]
  - [ sh, -c, 'aws secretsmanager get-secret-value --secret-id gh-token
        --query SecretString --output text > /run/secrets/gh-token' ]
  - [ sh, -c, 'sudo -u <user> -i env RUN_BASH_HEADLESS=1
        RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=<gh-username>
        RUN_BASH_GITHUB_TOKEN_FILE=/run/secrets/gh-token
        RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass
        /home/<user>/run.bash' ]
```

Replace `<user>` with the box's non-root distro user. When fetching `run.bash` itself,
pin it to a commit SHA (not `HEAD`) and inspect before running — do not pipe it
straight into a shell.

## Fail-fast, fail-loud guarantee

A headless run never hangs and never half-provisions silently:

- Any missing required value or unmet precondition aborts in **preflight**, before any
  provisioning action, with a message naming the exact fix.
- Any failure **during** provisioning (token rejected, SSH key load, vault mismatch,
  main playbook failure, a requested optional play missing or failing) aborts with a
  **big red banner** naming the step, the concrete reason, and a debug pointer — then
  exits non-zero. It never reports success on failure and never continues past a
  main-playbook failure.

## See also

- `./run.bash --help` and `./run.bash --help-run-headless` — the built-in, authoritative contract
- [Installation Guide](installation.md) — the interactive desktop install
- [GitHub Multi-Account Setup](github-multi-account.md) — the account model referenced by `github_accounts`
